# Oura data replicator

This is a Go program that extracts numerical data from the Oura API
and sends it to a Graphite timeseries database.

It uses the Oauth client API, which is good and bad:

+ It can do many people at once.  That's good.
+ To run it, you have to register with Oura to get a unique ClientID
  and ClientSecret.  That's bad.
+ You don't have to do anything to provision or manage users; they use
  the "sign in with Oura" oauth flow.  That's good.
+ It has to be somewhere that is online and reachable all the time,
  because that is how oauth and the webhook subscriptions work.
  That's bad.
+ Can I go now?

See "Similar Projects" below for a simpler program that works for one
person.

# Supported data types and output format

The Graphite timeseries hierarchy comes out like this:

`bio.${username}.${document_type}.${metric}`

where:

+ `${username}` is what a person selected for themselves when they
  signed up
+ `${document_type}` is activity, hr, readiness, or sleep (these 4 are
  what is implemented)
+ `${metric}` is an element from the given document.  Anything numeric
  that appears in the [Oura v2 API](https://cloud.ouraring.com/v2/docs)
  is mapped.

The "documents" supplied by the API are basically the same as the
cards that the phone app shows you, minus the peppy words.  But with
the numbers stored as timeseries, you can recombine the data in other
ways and make up whatever weird graphs or visualizations you want.

# How to run your own

You need at least the following:

+ A place to host this binary 24/7 that is reachable with a public
  HTTPS URL.
+ A Graphite server to receive the data stream.  This should be
  reachable by the Go binary.  It is possible to dump the data to a
  log file and ingest it to graphite later.
+ A Grafana server or whatever else you use with Graphite.
+ An Oauth ClientID and ClientSecret that you obtain by [registering
  with Oura](https://cloud.ouraring.com/oauth/applications).

I run it in a docker container on a tiny EC2 instance with a
certificate from LetsEncrypt.  I have nginx terminating SSL; it's not
implemented in this binary.  The nginx config looks like:

```
server {
  listen 443 ssl http2;
  server_name my.server.name;
  access_log /var/log/nginx/access_oura.log main;
  ssl_certificate "/etc/letsencrypt/live/my.server.name/fullchain.pem";
  ssl_certificate_key "/etc/letsencrypt/live/my.server.name/privkey.pem";
  location / {
    proxy_pass http://127.0.0.1:8000/;
  }
}
```

The included Dockerfile is an example of how to build a container and
run it.

There is an example dashboard that can be imported into Grafana at
`examples/grafana_leaderboard.json`.

# Learnings about the Oura API

There is a lot of room for improvement in the Oura API documentation.
Here is what I had to find out by trial and error.

## Oura's Oauth2 service

The Oura Oauth2 auth server is basically fine.  It works with the
standard-ish Go library `golang.org/x/oauth2`.  Oura AccessTokens only
have a lifetime of one day, and you get one shot to use the
RefreshToken.  The Go library has a problem that makes this a lot more
difficult: https://github.com/golang/oauth2/issues/8

The workaround in use here is equivalent to the one by @dnesting.

The auth server does not appear to care if you issue new tokens to
yourself dozens of times a day, which is good when you are trying to
get the aforementioned complicated workaround to work.

There is an undocumented Oauth scope named "stress" that is required
by the `daily_resilience` routes.  It is *not* required by the
`daily_stress` endpoint.

## Polling for documents

Most of the available endpoints (which they call "routes") are
organized around JSON documents that are updated periodically inside
Oura.  Most likely, they have a mongodb full of these, and batch jobs
recalculate the docs when there is new raw data from a ring.

For each document type, the route named **Multiple XYZ Documents**
functions as a search.  You specify a date range and get all the
documents inside your date range.  The user is implicitly identifed by
the `Authorization: Bearer` header that you supply.  This is either a
Personal Access Token or an oauth2 Access Token.

Here is an example response from `daily_readiness`.  It will not be
pretty-printed; I did that:

```
{
  "data": [
    {
      "id": "31ecbf2e-b627-46cb-b7e2-70fb90bf4ecf",
      "contributors": {
        "activity_balance": 81,
        "body_temperature": 95,
        "hrv_balance": 65,
        "previous_day_activity": 71,
        "previous_night": 51,
        "recovery_index": 42,
        "resting_heart_rate": 54,
        "sleep_balance": 62
      },
      "day": "2024-06-25",
      "score": 62,
      "temperature_deviation": -0.29,
      "temperature_trend_deviation": 0.04,
      "timestamp": "2024-06-25T00:00:00+00:00"
    }
  ]
}
```

The `examples/` directory has example responses from most of the
document routes.

Each document type also has a route named **Single XYZ Document**.
For this you append a document ID to the URL and receive only that
document.  This is useless unless you are using webhook notifications,
because you have no way to find out the document IDs.

## Webhook/subscription API

If there is any way that polling is good enough for you, just do that.
It is about 100x simpler than getting "webhook" notifications to work.

The webhook routes work in a completely different style from the
document routes.  You do not send any `Authorization:` header with an
Oauth token.  You do however need an Oauth app registration, because
you send weird custom headers `x-client-id` and `x-client-secret`.

Then you have to get through a weird verification protocol.  You make
a PUT (?) request for a new subscription, and something is supposed to
connect back to your callback URL (??) with a random string in a query
parameter.  You prove your worth by taking that string and sending it
back in a JSON object. (???)  Then the original PUT request, which has
been hanging until now (????) is supposed to return HTTP 201 (?????).

This is as fragile as it sounds.  One problem is that the callback
fails unless your server presents the entire certificate chain
(i.e. `fullchain.pem` from LetsEncrypt), NOT the certificate itself.
Your only clue is a PUT response of HTTP 500 with a body of
"SSLError."  Oura's support people did not know how to solve this; a
friend eventually guessed the problem.

The next problem is that the callback often times out inside Oura.  It
can take several minutes before you receive the callback.  But they
have a CloudFront WAF/?????? in front of your PUT request, which will
time out and kill it after 60 seconds.  So it often happens that you
send a PUT, wait 60 seconds, get a Cloudflare 504 boilerplate error,
which your JSON parser can't parse by the way, and then you'll see the
callback run uselessly a few minutes later.  I don't think there is
any solution other than just try again and eventually get lucky.

If you get this far, you will figure out that the "subscription" is
associated with your callback URL, a data type
(e.g. `daily_readiness`) and an event type (e.g. `update`) ONLY.
Subscriptions are not per-user-ID.  The Oauth token or PAT that you
probably went to a lot of trouble to include, is doing nothing.  Oura
somehow remembers on its end all the UIDs you are interested in, and
when you have a subscription, you will receive notifications for all
of them.  This isn't explained in the documentation, and how it
decides what UIDs to send you is undefined.

When a subscription is working, you will receive POST objects as shown
in the documentation:

```
{
  "event_type": "update",
  "data_type": "tag",
  "object_id": "9fc867f2-b455-4c41-a05a-751c6e764ffa",
  "event_time": "2022-11-16T08:21:00+00:00",
  "user_id": "bd913327d56d-a0adf03b515a1d8ed46082e"
}
```

To get the updated document, you need to use the document-oriented API
routes with the oauth token that corresponds to that `user_id`.  You
need to have called the `personal_info` route for each token and saved
the results somewhere.  There is no other way to know what oauth
identity you are talking about.  This must be why the `personal_info`
route ignores scopes and works on any token.

Subscriptions are given an expiration of about 90 days.  So as of this
writing, I don't know if my "renew" process works, because I won't
find out for 3 months.  (And if it fails, it will be another 3 months
to test the fix.)

## Oddities and inconsistencies

### timeseries values

TODO

### daily_spo2

The `daily_spo2` document has two differences from all the others.

```
  {
    "id": "345ad0b7-bf23-4e12-8e21-5005f53f8432",
    "day": "2024-08-09",
    "spo2_percentage": {
      "average": 97.804
    }
  }
```

It has no Timestamp, and it contains a pointless nested data
structure.  The `daily_spo2` search API route also has a different
behavior, where it sometimes returns content-free documents where the
`average` member is null.

### daily_resilience

The `daily_resilience` route has four differences from all the others.

Other daily documents contain a numeric `score`, but this one is named
`level` and it is a string such as "adequate".  I translate them to
{1,2,3,4,5}.

The `daily_resilience` doc has a map named `contributors`, but unlike
all the others, these are floats.  You cannot discover this from the
documentation because it uses 0 as the example values.

The resilience routes require an undocumented oauth scope named
"stress".  At least the error message tells you about it:

```
HTTP 401

{"detail":"Token is not authorized access stress scope."}
```

Finally, there is no subscription for `daily_resilience`
notifications.  If you try to create one, it will say:

```
HTTP 422 Unprocessable Entity

{
  "detail": [
    {
      "type": "enum",
      "loc": [
        "body",
        "data_type"
      ],
      "msg": "Input should be 'tag', 'enhanced_tag', 'workout', 'session', 'sleep', 'daily_sleep', 'daily_readiness', 'daily_activity', 'daily_spo2', 'sleep_time', 'rest_mode_period', 'ring_configuration', 'daily_stress' or 'daily_cycle_phases'",
      "input": "daily_resilience",
      "ctx": {
        "expected": "'tag', 'enhanced_tag', 'workout', 'session', 'sleep', 'daily_sleep', 'daily_readiness', 'daily_activity', 'daily_spo2', 'sleep_time', 'rest_mode_period', 'ring_configuration', 'daily_stress' or 'daily_cycle_phases'"
      },
      "url": "https://errors.pydantic.dev/2.7/v/enum"
    }
  ]
}
```

I guess you can only poll for this document type.

# Similar projects

A person possibly named Sam Roberts has written a thing in Python that
saves to a postgres database:

https://github.com/sam-roberts/Oura-data-visualiser
https://www.reddit.com/r/ouraring/comments/148t9eh/i_made_a_tool_that_pulls_my_data_from_oura_api/

It does this with 1/8 the code, because (a) python and (b) it uses the
Personal Access Token scheme, which is far simpler but only works for
one person.  If only want to replicate your own data, it will be
easier and more reliable to use their thing.