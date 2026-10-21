package store

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

type IssueTagTopValue struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type IssueTag struct {
	Key          string             `json:"key"`
	Name         string             `json:"name"`
	UniqueValues int64              `json:"uniqueValues"`
	TotalValues  int64              `json:"totalValues"`
	TopValues    []IssueTagTopValue `json:"topValues"`
}

func issueTagName(key string) string {
	name := strings.ReplaceAll(strings.ReplaceAll(key, "_", " "), "-", " ")
	words := strings.Fields(name)
	for i, word := range words {
		runes := []rune(word)
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func (s *Store) IssueTags(ctx context.Context, userID, id string) ([]IssueTag, error) {
	tx, issue, _, _, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH scoped AS (
 SELECT e.id,e.data->'tags' AS tags FROM events e
 WHERE e.issue_id IN (SELECT id FROM issues WHERE id=$1::bigint OR merged_into=$1::bigint)
   AND e.data ? 'tags'
), pairs AS (
 SELECT DISTINCT s.id,p.key,p.value
 FROM scoped s
 CROSS JOIN LATERAL (
  SELECT x.key AS key,x.value #>> '{}' AS value FROM jsonb_each(CASE WHEN jsonb_typeof(s.tags)='object' THEN s.tags ELSE '{}'::jsonb END) x
  WHERE jsonb_typeof(x.value)='string'
  UNION ALL
  SELECT value->>0 AS key,value->>1 AS value FROM jsonb_array_elements(CASE WHEN jsonb_typeof(s.tags)='array' THEN s.tags ELSE '[]'::jsonb END) value
  WHERE jsonb_typeof(value)='array' AND jsonb_array_length(value)=2 AND jsonb_typeof(value->1)='string'
 ) p
 WHERE length(p.key) BETWEEN 1 AND 200
)
SELECT key,value,count(*) FROM pairs GROUP BY key,value ORDER BY key,count(*) DESC,value`, issue.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]*IssueTag{}
	for rows.Next() {
		var key, value string
		var count int64
		if err := rows.Scan(&key, &value, &count); err != nil {
			return nil, err
		}
		tag := byKey[key]
		if tag == nil {
			tag = &IssueTag{Key: key, Name: issueTagName(key)}
			byKey[key] = tag
		}
		tag.UniqueValues++
		tag.TotalValues += count
		if len(tag.TopValues) < 5 {
			tag.TopValues = append(tag.TopValues, IssueTagTopValue{Key: key, Name: tag.Name, Value: value, Count: count})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]IssueTag, 0, len(byKey))
	for _, tag := range byKey {
		result = append(result, *tag)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
