package store

import "fmt"

// issueSearchPredicates separates current issue state from predicates on one occurrence.
func issueSearchPredicates(search IssueSearch, args *[]any) (issuePredicates, eventPredicates []string, err error) {
	bind := func(value string) string { *args = append(*args, value); return fmt.Sprintf("$%d", len(*args)) }
	if search.Text != "" {
		parameter := bind("%" + issueSearchPattern(search.Text, false) + "%")
		eventPredicates = append(eventPredicates, "(e.title ILIKE "+parameter+` ESCAPE E'\\' OR e.data::text ILIKE `+parameter+` ESCAPE E'\\')`)
	}
	for _, term := range search.Terms {
		var predicate string
		switch term.Field {
		case "status":
			predicate = issueSearchValues("i.status", term.Values, args)
		case "environment":
			predicate = issueSearchValues("e.environment", term.Values, args)
		case "release":
			predicate = issueSearchValues("e.release", term.Values, args)
		case "level":
			predicate = issueSearchValues("e.level", term.Values, args)
		case "transaction":
			predicate = issueSearchValues("e.transaction_name", term.Values, args)
		case "tag":
			key := bind(term.Key)
			predicate = `EXISTS(SELECT 1 FROM (
             SELECT object_tag.value FROM jsonb_each(CASE WHEN jsonb_typeof(e.data->'tags')='object'
                 THEN e.data->'tags' ELSE '{}'::jsonb END) object_tag WHERE object_tag.key=` + key + `
             UNION ALL
             SELECT pair.value->1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(e.data->'tags')='array'
                 THEN e.data->'tags' ELSE '[]'::jsonb END) pair
             WHERE jsonb_array_length(CASE WHEN jsonb_typeof(pair.value)='array' THEN pair.value ELSE '[]'::jsonb END)=2
                 AND jsonb_typeof(pair.value->0)='string' AND pair.value->>0=` + key + `
         ) tag WHERE jsonb_typeof(tag.value)='string' AND ` + issueSearchValues("(tag.value #>> '{}')", term.Values, args) + `)`
		default:
			return nil, nil, fmt.Errorf("%w: unknown search field", ErrInvalidIssueFilter)
		}
		if term.Negated {
			predicate = "NOT(" + predicate + ")"
		}
		if term.Field == "status" {
			issuePredicates = append(issuePredicates, predicate)
		} else {
			eventPredicates = append(eventPredicates, predicate)
		}
	}
	return issuePredicates, eventPredicates, nil
}
