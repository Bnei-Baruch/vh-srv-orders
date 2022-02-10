FROM golang:1.14.14-stretch

RUN apt-get update && apt-get upgrade -y

MAINTAINER Himanshu Sadadiya

RUN mkdir /app

ADD . /app

WORKDIR /app

RUN go build -o main .

EXPOSE 8185

ENTRYPOINT /app/main --port 8185
