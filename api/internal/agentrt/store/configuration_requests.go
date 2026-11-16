package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
)

// Transaction ownership remains here; the complete query belongs to Delivery.
func ConfigurationRequests(ctx context.Context, db DB, limit int) ([]map[string]any, error) {
	out := []map[string]any{}
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = deliveryreadings.ConfigurationRequests(ctx, tx, limit)
		return err
	})
	return out, err
}
