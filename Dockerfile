ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/coordinator ./cmd/coordinator

FROM alpine:3.22
RUN adduser -D -H thirdshift
USER thirdshift
WORKDIR /app
COPY --from=build /out/coordinator /usr/local/bin/coordinator
EXPOSE 8080
ENTRYPOINT ["coordinator"]

