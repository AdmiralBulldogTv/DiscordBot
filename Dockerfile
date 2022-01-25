FROM golang:1.17.6 as builder

WORKDIR /tmp/discord-bot

COPY . .

ARG BUILDER
ARG VERSION

ENV DISCORD_BOT_BUILDER=${BUILDER}
ENV DISCORD_BOT_VERSION=${VERSION}

RUN apt-get update && apt-get install make git gcc -y && \
    make build_deps && \
    make

FROM alfg/ffmpeg:latest

WORKDIR /app

COPY --from=builder /tmp/discord-bot/bin/discord-bot .

CMD ["/app/discord-bot"]
