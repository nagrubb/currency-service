FROM golang:1.15.6 
MAINTAINER Nathan Grubb "me@nathangrubb.io"

RUN mkdir /service

ADD . /service/
WORKDIR /service
RUN go build -o main .

ENTRYPOINT ["/service/main"]
#CMD ["/app/service"]
