FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/user ./examples/06-shop-microservices/main/user \
 && go build -o /out/supplier ./examples/06-shop-microservices/main/supplier \
 && go build -o /out/order ./examples/06-shop-microservices/main/order

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/ /app/
