package store

import (
	"sort"
	"strconv"
	"time"
)

type traceIdentity struct {
	project int64
	source  int
	row     int64
	ordinal int
}

func (id traceIdentity) nodeID() string {
	if id.source == 2 {
		return "span-" + strconv.FormatInt(id.row, 10)
	}
	value := "event-" + strconv.FormatInt(id.row, 10)
	if id.source == 1 {
		value += "-" + strconv.Itoa(id.ordinal)
	}
	return value
}

type traceIndexNode struct {
	traceIdentity
	span, parent               string
	spanPresent, parentPresent bool
	start, end                 *time.Time
	parentIndex                int
	parentStatus               string
	children                   []int
	depth, position            int
}

func traceMilliseconds(start, end time.Time) float64 {
	return float64(end.Unix()-start.Unix())*1000 + float64(end.Nanosecond()-start.Nanosecond())/1e6
}

func traceInstant(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	stamp := value.UTC().Truncate(time.Microsecond)
	return &stamp
}

func traceForest(nodes []traceIndexNode, result *TraceDetail) []int {
	type localID struct {
		project int64
		span    string
	}
	local := make(map[localID][]int, len(nodes))
	global := make(map[string][]int, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		n.parentIndex = -1
		if n.span != "" {
			key := localID{n.project, n.span}
			local[key] = append(local[key], i)
			global[n.span] = append(global[n.span], i)
		} else if n.spanPresent {
			result.InvalidSpanIDCount++
		}
		for _, stamp := range []*time.Time{n.start, n.end} {
			if stamp == nil {
				continue
			}
			if result.StartTimestamp == nil || stamp.Before(*result.StartTimestamp) {
				result.StartTimestamp = stamp
			}
			if result.EndTimestamp == nil || stamp.After(*result.EndTimestamp) {
				result.EndTimestamp = stamp
			}
		}
		if n.start == nil || n.end == nil || n.end.Before(*n.start) {
			result.UntimedCount++
		}
	}
	for i := range nodes {
		n := &nodes[i]
		if !n.parentPresent {
			n.parentStatus = "none"
			continue
		}
		if n.parent == "" {
			n.parentStatus = "invalid"
			continue
		}
		candidates := local[localID{n.project, n.parent}]
		if len(candidates) == 0 {
			candidates = global[n.parent]
		}
		switch len(candidates) {
		case 0:
			n.parentStatus = "missing"
		case 1:
			n.parentIndex = candidates[0]
			n.parentStatus = "resolved"
		default:
			n.parentStatus = "ambiguous"
		}
	}
	state := make([]uint8, len(nodes))
	positions := make([]int, len(nodes))
	for first := range nodes {
		if state[first] != 0 {
			continue
		}
		path := []int{}
		current := first
		for current >= 0 && state[current] == 0 {
			state[current] = 1
			positions[current] = len(path)
			path = append(path, current)
			current = nodes[current].parentIndex
		}
		if current >= 0 && state[current] == 1 {
			for _, i := range path[positions[current]:] {
				nodes[i].parentIndex = -1
				nodes[i].parentStatus = "cycle"
			}
		}
		for _, i := range path {
			state[i] = 2
		}
	}
	roots := []int{}
	for i := range nodes {
		n := &nodes[i]
		if n.parentIndex >= 0 {
			nodes[n.parentIndex].children = append(nodes[n.parentIndex].children, i)
		} else {
			roots = append(roots, i)
		}
		switch n.parentStatus {
		case "none":
			result.Relationships.None++
		case "resolved":
			result.Relationships.Resolved++
		case "missing":
			result.Relationships.Missing++
		case "invalid":
			result.Relationships.Invalid++
		case "ambiguous":
			result.Relationships.Ambiguous++
		case "cycle":
			result.Relationships.Cycle++
		}
	}
	less := func(a, b int) bool {
		x, y := nodes[a], nodes[b]
		xTime, yTime := x.start, y.start
		if xTime == nil {
			xTime = x.end
		}
		if yTime == nil {
			yTime = y.end
		}
		if xTime == nil && yTime != nil {
			return false
		}
		if xTime != nil && yTime == nil {
			return true
		}
		if xTime != nil && !xTime.Equal(*yTime) {
			return xTime.Before(*yTime)
		}
		if x.project != y.project {
			return x.project < y.project
		}
		if x.source != y.source {
			return x.source < y.source
		}
		if x.row != y.row {
			return x.row < y.row
		}
		return x.ordinal < y.ordinal
	}
	sort.Slice(roots, func(i, j int) bool { return less(roots[i], roots[j]) })
	for i := range nodes {
		children := nodes[i].children
		sort.Slice(children, func(a, b int) bool { return less(children[a], children[b]) })
	}
	type step struct{ index, depth int }
	stack := []step{}
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, step{roots[i], 0})
	}
	order := make([]int, 0, len(nodes))
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &nodes[item.index]
		n.depth = item.depth
		n.position = len(order)
		order = append(order, item.index)
		for i := len(n.children) - 1; i >= 0; i-- {
			stack = append(stack, step{n.children[i], item.depth + 1})
		}
	}
	if result.StartTimestamp != nil {
		duration := traceMilliseconds(*result.StartTimestamp, *result.EndTimestamp)
		result.Duration = &duration
	}
	return order
}
