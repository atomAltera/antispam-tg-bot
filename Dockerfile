FROM golang:1.24-bullseye as build

WORKDIR /opt

RUN mkdir /opt/build

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o /opt/build/antispam-tg-bot nuclight.org/antispam-tg-bot/cmd/bot

FROM alpine:3.21

WORKDIR /opt/app

COPY --from=build /opt/build/antispam-tg-bot ./

ENTRYPOINT ["./antispam-tg-bot"]