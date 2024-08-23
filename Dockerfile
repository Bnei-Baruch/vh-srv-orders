FROM golang:1.21 AS base

# ARG here is to make the sha available for use in -ldflags
ARG GIT_SHA

WORKDIR /app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X gitlab.bbdev.team/vh/pay/orders/common.GitSHA=${GIT_SHA}" -o orders .

FROM alpine:latest

RUN apk --no-cache add curl

COPY misc/*.sh ./
COPY db /db
COPY --from=base /app/orders /

EXPOSE 8185

CMD ["./orders", "server"]
