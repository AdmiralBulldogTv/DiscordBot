FROM golang:1.17.7 as builder

WORKDIR /tmp/discord-bot

COPY . .

ARG BUILDER
ARG VERSION

ENV DISCORD_BOT_BUILDER=${BUILDER}
ENV DISCORD_BOT_VERSION=${VERSION}

RUN apt-get update && apt-get install make git gcc -y && \
    make build_deps && \
    make

FROM ubuntu:latest

WORKDIR /app

RUN apt-get update && apt-get install ca-certificates -y && rm -rf /var/lib/apt/lists/*

COPY --from=builder /tmp/discord-bot/bin/discord-bot .

CMD ["/app/discord-bot"]
