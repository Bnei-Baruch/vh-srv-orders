FROM golang:1.14.14-stretch

RUN apt-get update && apt-get upgrade -y

RUN mkdir /app

ADD . /app

WORKDIR /app

ARG PG_HOST=localhost
ENV PG_HOST=$PG_HOST
RUN echo $PG_HOST

ARG PG_USER=postgres
ENV PG_USER=$PG_USER
RUN echo $PG_USER

ARG PG_PWD=postgres
ENV PG_PWD=$PG_PWD
RUN echo $PG_PWD

RUN go build -o main .

EXPOSE 8185

ENTRYPOINT /app/main --port 8185
