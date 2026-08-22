FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/aruzor ./cmd/aruzor

FROM alpine:3.20
RUN adduser -D -H aruzor
WORKDIR /app
COPY --from=builder /out/aruzor .
RUN chown aruzor:aruzor /app
USER aruzor
ENV ARUZOR_LISTEN_ADDR=:8080
ENV ARUZOR_DB_PATH=/app/aruzor.db
EXPOSE 8080
ENTRYPOINT ["./aruzor"]
