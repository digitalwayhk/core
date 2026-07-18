FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/user ./examples/07-shop-order-scale/main/user \
 && go build -o /out/supplier ./examples/07-shop-order-scale/main/supplier \
 && go build -o /out/order ./examples/07-shop-order-scale/main/order

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/ /app/
