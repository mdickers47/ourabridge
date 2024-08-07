#FROM archlinux AS gobuild
#RUN pacman-key --init && \
#    pacman --noconfirm -Syy archlinux-keyring && \
#    pacman --noconfirm -Su && \
#    pacman --noconfirm -S base-devel git sudo go
FROM alpine AS gobuild
RUN apk update && apk add git go 
COPY . /opt
WORKDIR /opt
RUN go build .

FROM alpine AS prod
COPY --from=gobuild /opt/ourabridge /opt/ourabridge
WORKDIR /opt
CMD ["/opt/ourabridge"]
