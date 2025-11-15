package repository

import (
	"backend/internal/model"
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type OrderRepository struct {
	db DBTX
}

func NewOrderRepository(db DBTX) *OrderRepository {
	return &OrderRepository{db: db}
}

// 注文を作成し、生成された注文IDを返す
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) (string, error) {
	query := `INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES (?, ?, 'shipping', NOW())`
	result, err := r.db.ExecContext(ctx, query, order.UserID, order.ProductID)
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}

// 複数の注文IDのステータスを一括で更新
// 主に配送ロボットが注文を引き受けた際に一括更新をするために使用
func (r *OrderRepository) UpdateStatuses(ctx context.Context, orderIDs []int64, newStatus string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In("UPDATE orders SET shipped_status = ? WHERE order_id IN (?)", newStatus, orderIDs)
	if err != nil {
		return err
	}
	query = r.db.Rebind(query)
	_, err = r.db.ExecContext(ctx, query, args...)
	return err
}

// 配送中(shipped_status:shipping)の注文一覧を取得
func (r *OrderRepository) GetShippingOrders(ctx context.Context) ([]model.Order, error) {
	var orders []model.Order
	query := `
        SELECT
            o.order_id,
            p.weight,
            p.value
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.shipped_status = 'shipping'
    `
	err := r.db.SelectContext(ctx, &orders, query)
	return orders, err
}

// 注文履歴一覧を取得
func (r *OrderRepository) ListOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	// ステップ1: 総件数を取得（フィルター条件を適用）
	countQuery := `
        SELECT COUNT(*) as total
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.user_id = ?
    `
	args := []interface{}{userID}

	if req.Search != "" {
		if req.Type == "prefix" {
			countQuery += " AND p.name LIKE ?"
			args = append(args, req.Search+"%")
		} else {
			countQuery += " AND p.name LIKE ?"
			args = append(args, "%"+req.Search+"%")
		}
	}

	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	// ステップ2: ソートフィールドを決定
	sortField := "o.order_id"
	switch req.SortField {
	case "name", "product_name":
		sortField = "p.name"
	case "created_at":
		sortField = "o.created_at"
	case "shipped_status":
		sortField = "o.shipped_status"
	case "arrived_at":
		sortField = "o.arrived_at"
	case "order_id":
		sortField = "o.order_id"
	default:
		sortField = "o.order_id"
	}

	sortOrder := "ASC"
	if strings.ToUpper(req.SortOrder) == "DESC" {
		sortOrder = "DESC"
	}

	// ステップ3: ページングを適用したクエリを実行
	query := `
        SELECT o.order_id, o.product_id, o.shipped_status, o.created_at, o.arrived_at, p.name as product_name
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.user_id = ?
    `
	queryArgs := []interface{}{userID}

	if req.Search != "" {
		if req.Type == "prefix" {
			query += " AND p.name LIKE ?"
			queryArgs = append(queryArgs, req.Search+"%")
		} else {
			query += " AND p.name LIKE ?"
			queryArgs = append(queryArgs, "%"+req.Search+"%")
		}
	}

	// order_id 作為第二排序條件，但方向需與主要排序一致以穩定輸出
	if sortField == "o.order_id" {
		query += " ORDER BY " + sortField + " " + sortOrder
	} else {
		query += " ORDER BY " + sortField + " " + sortOrder + ", o.order_id " + sortOrder
	}
	query += " LIMIT ? OFFSET ?"
	queryArgs = append(queryArgs, req.PageSize, req.Offset)

	type orderRow struct {
		OrderID       int64        `db:"order_id"`
		ProductID     int          `db:"product_id"`
		ProductName   string       `db:"product_name"`
		ShippedStatus string       `db:"shipped_status"`
		CreatedAt     sql.NullTime `db:"created_at"`
		ArrivedAt     sql.NullTime `db:"arrived_at"`
	}
	var ordersRaw []orderRow
	if err := r.db.SelectContext(ctx, &ordersRaw, query, queryArgs...); err != nil {
		return nil, 0, err
	}

	var orders []model.Order
	for _, o := range ordersRaw {
		orders = append(orders, model.Order{
			OrderID:       o.OrderID,
			ProductID:     o.ProductID,
			ProductName:   o.ProductName,
			ShippedStatus: o.ShippedStatus,
			CreatedAt:     o.CreatedAt.Time,
			ArrivedAt:     o.ArrivedAt,
		})
	}

	return orders, total, nil
}
