package auth

import "context"

// PersonalData exports persisted identity attributes, excluding credentials.
// Table names are fixed here; account input is always a bound parameter.
func (r *UserRepository) PersonalData(ctx context.Context, username string) (map[string][]map[string]interface{}, error) {
	out := map[string][]map[string]interface{}{}
	for _, table := range []string{"users", "user_domain_entitlements", "user_quotas", "scim_identities"} {
		rows, err := r.db.QueryContext(ctx, "SELECT * FROM "+table+" WHERE username=$1", username)
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		items := []map[string]interface{}{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			dest := make([]interface{}, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, err
			}
			item := map[string]interface{}{}
			for i, name := range columns {
				if name == "password_hash" {
					continue
				}
				if b, ok := values[i].([]byte); ok {
					item[name] = string(b)
				} else {
					item[name] = values[i]
				}
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out[table] = items
	}
	return out, nil
}
