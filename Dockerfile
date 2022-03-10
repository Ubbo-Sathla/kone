FROM golang:1.17-alpine3.15 AS builder

ENV  GOPROXY https://goproxy.cn

WORKDIR /go/src/github.com/Ubbo-Sathla/kone

ADD . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/kone

RUN apk add upx && upx /bin/kone

FROM alpine:3.15.0

WORKDIR /kone

COPY --from=builder /bin/kone /bin/

ADD example.ini ./kone.ini

ENTRYPOINT ["/bin/kone","-config","/kone/kone.ini"]