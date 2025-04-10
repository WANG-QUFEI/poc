FROM public.ecr.aws/docker/library/golang:1.24-alpine AS builder

ENV CGO_ENABLED=0
ENV GOOS=linux

WORKDIR /

COPY . .

RUN go build -a -mod vendor -o poc ./cmd
RUN go build -a -mod vendor -o checksum_gen ./cmd/checksum

FROM public.ecr.aws/docker/library/alpine:3

WORKDIR /app

COPY db/migrations /migrations
COPY --from=builder /poc ./
COPY --from=builder /checksum_gen ./
COPY --from=ghcr.io/amacneil/dbmate:2.3.0 /usr/local/bin/dbmate /usr/local/bin/dbmate

EXPOSE 8080

ENTRYPOINT ["./poc"]

CMD [ "web_service" ]
