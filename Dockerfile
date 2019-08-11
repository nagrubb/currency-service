FROM golang:latest 
MAINTAINER Nathan Grubb "me@nathangrubb.io"

RUN mkdir /service
RUN go get github.com/gorilla/mux

ADD . /service/ 
WORKDIR /service
RUN go build -o main .

ENTRYPOINT ["/service/main"]
#CMD ["/app/service"]
