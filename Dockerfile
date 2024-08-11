# using this will look something like:
#
# docker create --name ourabridge \
#   --network host `# sad!` \
#   -p 8000:8000 \
#   -v ./client_creds.json:/opt/client_creds.json \
#   -v ./user_creds.json:/opt/user_creds.json \
#   -v ./data.txt:/opt/data.txt \
#   -e GRAPHITE_SRV=127.0.0.1:2003 \
#   ourabridge

FROM alpine AS gobuild
RUN apk update && apk add git go 
COPY . /opt
WORKDIR /opt
RUN go build .

FROM alpine AS prod
COPY --from=gobuild /opt/ourabridge /opt/ourabridge
WORKDIR /opt
CMD ["/opt/ourabridge"]
