package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service/utils"
	"context"
	"log"
	"sort"
)

type RobotService struct {
	store *repository.Store
}

func NewRobotService(store *repository.Store) *RobotService {
	return &RobotService{store: store}
}

// 注意：このメソッドは、現在、ordersテーブルのshipped_statusが"shipping"になっている注文"全件"を対象に配送計画を立てます。
// 注文の取得件数を制限した場合、ペナルティの対象になります。
func (s *RobotService) GenerateDeliveryPlan(ctx context.Context, robotID string, capacity int) (*model.DeliveryPlan, error) {
	var plan model.DeliveryPlan

	err := utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.ExecTx(ctx, func(txStore *repository.Store) error {
			orders, err := txStore.OrderRepo.GetShippingOrders(ctx)
			if err != nil {
				return err
			}
			plan, err = selectOrdersForDelivery(ctx, orders, robotID, capacity)
			if err != nil {
				return err
			}
			if len(plan.Orders) > 0 {
				orderIDs := make([]int64, len(plan.Orders))
				for i, order := range plan.Orders {
					orderIDs[i] = order.OrderID
				}

				if err := txStore.OrderRepo.UpdateStatuses(ctx, orderIDs, "delivering"); err != nil {
					return err
				}
				log.Printf("Updated status to 'delivering' for %d orders", len(orderIDs))
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *RobotService) UpdateOrderStatus(ctx context.Context, orderID int64, newStatus string) error {
	return utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.OrderRepo.UpdateStatuses(ctx, []int64{orderID}, newStatus)
	})
}

func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, robotCapacity int) (model.DeliveryPlan, error) {
	if robotCapacity <= 0 || len(orders) == 0 {
		return model.DeliveryPlan{RobotID: robotID}, nil
	}

	candidates := append([]model.Order(nil), orders...)
	sort.SliceStable(candidates, func(i, j int) bool {
		wi, wj := candidates[i].Weight, candidates[j].Weight
		vi, vj := candidates[i].Value, candidates[j].Value
		if wi == 0 && wj == 0 {
			return vi > vj
		}
		if wi == 0 {
			return true
		}
		if wj == 0 {
			return false
		}
		return vi*wj > vj*wi
	})

	bestValue := 0
	var bestSet []model.Order
	if val, set := greedySeedPlan(candidates, robotCapacity); val > 0 {
		bestValue = val
		bestSet = set
	}

	steps := 0
	checkEvery := 16384

	fractionalBound := func(idx, curWeight int) float64 {
		if curWeight >= robotCapacity {
			return 0
		}
		remaining := robotCapacity - curWeight
		bound := 0.0
		for j := idx; j < len(candidates) && remaining >= 0; j++ {
			order := candidates[j]
			if order.Weight <= 0 {
				bound += float64(order.Value)
				continue
			}
			if order.Weight <= remaining {
				remaining -= order.Weight
				bound += float64(order.Value)
			} else {
				frac := float64(order.Value) * float64(remaining) / float64(order.Weight)
				bound += frac
				break
			}
		}
		return bound
	}

	var dfs func(i, curWeight, curValue int, curSet []model.Order) bool
	dfs = func(i, curWeight, curValue int, curSet []model.Order) bool {
		if curWeight > robotCapacity {
			return false
		}

		steps++
		if checkEvery > 0 && steps%checkEvery == 0 {
			select {
			case <-ctx.Done():
				return true
			default:
			}
		}

		if float64(curValue)+fractionalBound(i, curWeight) <= float64(bestValue) {
			return false
		}

		if curValue > bestValue {
			bestValue = curValue
			bestSet = append([]model.Order(nil), curSet...)
		}

		if i == len(candidates) {
			return false
		}

		order := candidates[i]

		if dfs(i+1, curWeight+order.Weight, curValue+order.Value, append(curSet, order)) {
			return true
		}

		return dfs(i+1, curWeight, curValue, curSet)
	}

	canceled := dfs(0, 0, 0, nil)
	if canceled {
		return model.DeliveryPlan{}, ctx.Err()
	}

	var totalWeight int
	for _, o := range bestSet {
		totalWeight += o.Weight
	}

	return model.DeliveryPlan{
		RobotID:     robotID,
		TotalWeight: totalWeight,
		TotalValue:  bestValue,
		Orders:      bestSet,
	}, nil
}

func greedySeedPlan(orders []model.Order, capacity int) (int, []model.Order) {
	remaining := capacity
	value := 0
	selected := make([]model.Order, 0, len(orders))
	for _, o := range orders {
		if o.Weight <= 0 {
			value += o.Value
			selected = append(selected, o)
			continue
		}
		if o.Weight <= remaining {
			remaining -= o.Weight
			value += o.Value
			selected = append(selected, o)
		}
	}
	return value, append([]model.Order(nil), selected...)
}
