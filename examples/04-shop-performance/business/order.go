package business

import (
	"strings"

	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// OrderService 处理下单、查询、删除和撤销申请。
type OrderService struct{}

// NewOrderService 创建无状态订单业务服务。
func NewOrderService() *OrderService { return &OrderService{} }

// CreateOrder 使用商品事实数据创建用户订单快照。
// orderID 须由接口层 req.NewID() 提供，作为 GetHash / 主键。
func (own *OrderService) CreateOrder(userID string, productID uint, quantity int, orderID uint) (*OrderChange, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, models.NewBusinessError("用户身份无效")
	}
	if orderID == 0 {
		return nil, models.NewValidationError("订单 ID 不能为空")
	}
	if quantity <= 0 {
		return nil, models.NewValidationError("订单数量必须大于 0")
	}
	// 商品、供应商和价格是下单时的事实快照。服务启动后从本地事实缓存读取，
	// 冷加载由 go-zero SingleFlight 合并；Manage 变更通过 EventBridge 立即失效。
	reference, err := getOrderReference(productID)
	if err != nil {
		return nil, err
	}
	order := models.NewOrder()
	order.SetID(orderID)
	order.ProductID = reference.ProductID
	order.ProductCode = reference.ProductCode
	order.ProductName = reference.ProductName
	order.SupplierID = reference.SupplierID
	order.SupplierCode = reference.SupplierCode
	order.SupplierName = reference.SupplierName
	order.UnitPrice = reference.UnitPrice
	order.Quantity = quantity
	order.UserID = userID
	if err := order.Insert(); err != nil {
		return nil, err
	}
	return &OrderChange{Action: "created", Order: order}, nil
}

// ListUserOrders 查询当前用户全部订单。
func (own *OrderService) ListUserOrders(userID string) ([]*models.Order, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, models.NewBusinessError("用户身份无效")
	}
	return models.QueryVisibleOrders(userID)
}

// DeleteUnpaidOrder 只物理删除未支付或支付失败的本人订单。
func (own *OrderService) DeleteUnpaidOrder(userID string, orderID uint) (*OrderChange, error) {
	order, err := own.findOwned(orderID, userID)
	if err != nil {
		return nil, err
	}
	switch order.PaymentStatus {
	case models.PaymentStatusUnpaid, models.PaymentStatusFailed:
		if err := order.Delete(); err != nil {
			return nil, err
		}
		return &OrderChange{Action: "deleted", Order: order}, nil
	case models.PaymentStatusPending:
		return nil, models.NewBusinessError("支付处理中，不能删除或撤销订单")
	default:
		return nil, models.NewBusinessError("已支付订单不能删除，只能申请撤销")
	}
}

// RequestCancellation 把已支付订单和当前流水同时置为退款中。
func (own *OrderService) RequestCancellation(userID string, orderID uint) (*OrderChange, error) {
	if err := models.FlushPendingOrder(userID, orderID); err != nil {
		return nil, err
	}
	var order *models.Order
	err := models.RunInTransaction(func(action persistencetypes.IDataAction) error {
		var err error
		order, err = own.findOwnedWith(action, orderID, userID)
		if err != nil {
			return err
		}
		if order.OrderStatus() == models.OrderStatusCancelling && order.PaymentStatus == models.PaymentStatusRefunding {
			return nil
		}
		if order.OrderStatus() != models.OrderStatusNormal || order.PaymentStatus != models.PaymentStatusPaid {
			return models.NewBusinessError("只有已支付订单可以申请撤销")
		}
		payment, err := models.NewPaymentRecord().FindByIDWith(action, order.PaymentID)
		if err != nil {
			return err
		}
		if payment == nil || payment.PaymentStatus() != models.PaymentStatusPaid {
			return models.NewBusinessError("订单支付流水状态不一致")
		}
		order.Status = int(models.OrderStatusCancelling)
		order.PaymentStatus = models.PaymentStatusRefunding
		payment.Status = int(models.PaymentStatusRefunding)
		if err := payment.UpdateWith(action); err != nil {
			return err
		}
		return order.UpdateWith(action)
	})
	if err != nil {
		return nil, err
	}
	return &OrderChange{Action: "refund_pending", Order: order}, nil
}

// findOwned 使用统一错误隐藏其他用户的订单存在性。
func (own *OrderService) findOwned(orderID uint, userID string) (*models.Order, error) {
	if err := models.FlushPendingOrder(userID, orderID); err != nil {
		return nil, err
	}
	return own.findOwnedWith(nil, orderID, userID)
}

// findOwnedWith 使用可选的事务适配器查询本人订单。
func (own *OrderService) findOwnedWith(action persistencetypes.IDataAction, orderID uint, userID string) (*models.Order, error) {
	var (
		order *models.Order
		err   error
	)
	if action == nil {
		order, err = models.NewOrder().FindOwned(orderID, strings.TrimSpace(userID))
	} else {
		order, err = models.NewOrder().FindOwnedWith(action, orderID, strings.TrimSpace(userID))
	}
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, models.NewBusinessError("订单不存在或无权操作")
	}
	return order, nil
}
