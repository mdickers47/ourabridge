FROM alpine AS gobuild
RUN apk update && apk add git go 
COPY . /opt
WORKDIR /opt
RUN go build .

FROM alpine AS prod
COPY --from=gobuild /opt/ourabridge /opt/ourabridge
WORKDIR /opt
CMD ["/opt/ourabridge"]
