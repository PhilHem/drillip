FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /drillip .

FROM scratch
COPY --from=build /drillip /drillip
VOLUME /data
ENV DRILLIP_DB=/data/errors.db
ENV DRILLIP_ADDR=0.0.0.0:8300
EXPOSE 8300
ENTRYPOINT ["/drillip"]
