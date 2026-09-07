package jmap

import "math"

// queryPage implements RFC 8620 section 5.5 positioning after filtering and
// sorting. Keep the response position separate from the bounded slice index:
// a position beyond the end is valid and must not be rewritten to the total.
func queryPage(args map[string]interface{}, ids []string) (position, start, end int, kind string) {
	integer := func(key string) (int, bool) {
		v, exists := args[key]
		if !exists {
			return 0, true
		}
		n, ok := v.(float64)
		// JMAP Int uses the JSON safe integer range. Bound conversion for both
		// 32- and 64-bit platforms, leaving room for adding the result count.
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < -9007199254740991 || n > 9007199254740991 || n >= float64(int(^uint(0)>>1))-float64(len(ids)) || n < -float64(int(^uint(0)>>1)) {
			return 0, false
		}
		return int(n), true
	}
	if anchor := args["anchor"]; anchor != nil {
		anchor, ok := anchor.(string)
		if !ok || anchor == "" {
			return 0, 0, 0, "invalidArguments"
		}
		offset, ok := integer("anchorOffset")
		if !ok {
			return 0, 0, 0, "invalidArguments"
		}
		found := false
		for i, id := range ids {
			if id == anchor {
				position, found = i+offset, true
				break
			}
		}
		if !found {
			return 0, 0, 0, "anchorNotFound"
		}
	} else {
		var ok bool
		position, ok = integer("position")
		if !ok {
			return 0, 0, 0, "invalidArguments"
		}
		if position < 0 {
			position += len(ids)
		}
	}
	if position < 0 {
		position = 0
	}
	limit, ok := numericLimit(args, "limit", maxJMAPObjects)
	if !ok {
		return 0, 0, 0, "invalidArguments"
	}
	if limit > maxJMAPObjects {
		limit = maxJMAPObjects
	}
	start = position
	if start > len(ids) {
		start = len(ids)
	}
	end = start + min(limit, len(ids)-start)
	return position, start, end, ""
}
