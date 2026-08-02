# Stage 1: Builder
# Base images come from the AWS ECR Public MIRROR of Docker Official Images —
# byte-identical, but WITHOUT Docker Hub's anonymous pull rate-limits, which
# cause "[internal] load metadata ... context canceled" build failures on
# shared CI/build IPs (Railway/Render).
# NOTE: golang:1.25-alpine3.20 no longer exists upstream — alpine 3.21 it is.
FROM public.ecr.aws/docker/library/golang:1.25-alpine3.21 AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY . .

RUN go build -ldflags="-w -s" -o premiumcard .

# Stage 2: Final image
FROM public.ecr.aws/docker/library/alpine:3.21

RUN apk --no-cache add ca-certificates && \
    apk update && apk upgrade --available && sync

COPY --from=builder /app/premiumcard /premiumcard

# DB (SQLite) is written to /data/bot.db — mount a volume to persist it:
#   docker run -v premiumcard-data:/data ...
WORKDIR /data

ENTRYPOINT ["/premiumcard"]
