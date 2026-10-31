// 本文件是 07 独立 go-zero 对照的角色入口，不调用 Core 服务或业务封装。
package main

import (
	"flag"
	"log"
	"os"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/go-zero-reference/internal/reference"
)

func main() {
	port := flag.Int("p", 18181, "HTTP 端口")
	grpcPort := flag.Int("grpc", 28181, "内部 RPC 端口")
	flag.Parse()
	c := reference.Config{Role: os.Getenv("REFERENCE_ROLE"), HTTPPort: *port, GRPCPort: *grpcPort, CoreJSON: os.Getenv("REFERENCE_CONFIG_FILE"), SupplierDB: os.Getenv("REFERENCE_SUPPLIER_DB"), SupplierRPC: os.Getenv("REFERENCE_SUPPLIER_RPC"), OrderRPC: os.Getenv("REFERENCE_ORDER_RPC"), PendingDir: os.Getenv("SHOP_LOCAL_PENDING_DIR")}
	if c.Role != "user" && c.Role != "supplier" && c.Role != "order" {
		log.Fatal("REFERENCE_ROLE 只接受 user、supplier、order")
	}
	if e := reference.Run(c); e != nil {
		log.Fatal(e)
	}
}
