FROM golang:1.17-alpine3.15 AS builder

ARG GOPROXY=https://goproxy.cn
ARG REPO=mirrors.ustc.edu.cn

ENV  GOPROXY $GOPROXY

WORKDIR /go/src/github.com/Ubbo-Sathla/kone

ADD . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/kone

RUN sed -i "s/dl-cdn.alpinelinux.org/${REPO}/g" /etc/apk/repositories && \
    apk add upx && upx /bin/kone

FROM alpine:3.15.0

WORKDIR /kone

COPY --from=builder /bin/kone /bin/

ADD example.ini ./kone.ini

ENTRYPOINT ["/bin/kone","-config","/kone/kone.ini"]