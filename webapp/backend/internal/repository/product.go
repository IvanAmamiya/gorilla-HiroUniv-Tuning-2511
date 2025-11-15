package repository

import (
	"backend/internal/model"
	"context"
	"strings"
)

type ProductRepository struct {
	db DBTX
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{db: db}
}

// 商品一覧を取得（DB側で検索・ソート・ページングを完結）
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	baseQuery := `
		SELECT product_id, name, value, weight, image, description
		FROM products
	`
	whereClause := ""
	args := make([]interface{}, 0)

	if req.Search != "" {
		pattern := "%" + req.Search + "%"
		if strings.ToLower(req.Type) == "prefix" {
			pattern = req.Search + "%"
		}
		whereClause = " WHERE (name LIKE ? OR description LIKE ?)"
		args = append(args, pattern, pattern)
	}

	countQuery := "SELECT COUNT(*) FROM products" + whereClause
	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	query := baseQuery + whereClause

	sortField := "product_id"
	switch req.SortField {
	case "name":
		sortField = "name"
	case "value":
		sortField = "value"
	case "weight":
		sortField = "weight"
	case "product_id":
		fallthrough
	default:
		sortField = "product_id"
	}

	sortOrder := "ASC"
	if strings.ToUpper(req.SortOrder) == "DESC" {
		sortOrder = "DESC"
	}

	if sortField == "product_id" {
		query += " ORDER BY " + sortField + " " + sortOrder
	} else {
		query += " ORDER BY " + sortField + " " + sortOrder + ", product_id ASC"
	}
	query += " LIMIT ? OFFSET ?"
	queryArgs := append(append([]interface{}{}, args...), req.PageSize, req.Offset)

	var products []model.Product
	if err := r.db.SelectContext(ctx, &products, query, queryArgs...); err != nil {
		return nil, 0, err
	}

	return products, total, nil
}
