FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /error-sink .

FROM scratch
COPY --from=build /error-sink /error-sink
VOLUME /data
ENV ERROR_SINK_DB=/data/errors.db
ENV ERROR_SINK_ADDR=0.0.0.0:8300
EXPOSE 8300
ENTRYPOINT ["/error-sink"]
