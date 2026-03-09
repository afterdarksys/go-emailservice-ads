package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// CreateIndexTemplate creates the index template for mail events
func (c *Client) CreateIndexTemplate(ctx context.Context) error {
	templateName := fmt.Sprintf("%s-template", c.indexPrefix)

	template := map[string]interface{}{
		"index_patterns": []string{fmt.Sprintf("%s-*", c.indexPrefix)},
		"template": map[string]interface{}{
			"settings": map[string]interface{}{
				"number_of_shards":   c.config.Elasticsearch.Shards,
				"number_of_replicas": c.config.Elasticsearch.Replicas,
				"index": map[string]interface{}{
					"refresh_interval": "5s",
					"codec":            "best_compression",
				},
				"analysis": map[string]interface{}{
					"analyzer": map[string]interface{}{
						"email_analyzer": map[string]interface{}{
							"type":      "custom",
							"tokenizer": "uax_url_email",
							"filter":    []string{"lowercase"},
						},
					},
				},
			},
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					// Core identifiers
					"message_id":          fieldKeyword(),
					"original_message_id": fieldKeyword(),
					"queue_id":            fieldKeyword(),
					"trace_id":            fieldKeyword(),
					"parent_trace_id":     fieldKeyword(),
					"instance_id":         fieldKeyword(),
					"region":              fieldKeyword(),
					"deployment_mode":     fieldKeyword(),

					// Event metadata
					"event_type": fieldKeyword(),
					"timestamp":  fieldDate(),
					"tier":       fieldKeyword(),

					// Envelope
					"envelope": map[string]interface{}{
						"properties": map[string]interface{}{
							"from":       fieldEmail(),
							"to":         fieldEmail(),
							"size_bytes": fieldLong(),
						},
					},

					// Metadata
					"metadata": map[string]interface{}{
						"properties": map[string]interface{}{
							"content_hash":       fieldKeyword(),
							"client_ip":          fieldIP(),
							"authenticated_user": fieldKeyword(),
							"helo_hostname":      fieldKeyword(),
							"received_at":        fieldDate(),
						},
					},

					// Security
					"security": map[string]interface{}{
						"properties": map[string]interface{}{
							"spf_result":    fieldKeyword(),
							"dkim_result":   fieldKeyword(),
							"dmarc_result":  fieldKeyword(),
							"dane_verified": fieldBoolean(),
							"tls_version":   fieldKeyword(),
							"greylisted":    fieldBoolean(),
						},
					},

					// Delivery
					"delivery": map[string]interface{}{
						"properties": map[string]interface{}{
							"remote_host":    fieldKeyword(),
							"remote_ip":      fieldIP(),
							"smtp_code":      fieldInteger(),
							"latency_ms":     fieldLong(),
							"attempt_number": fieldInteger(),
							"next_retry_at":  fieldDate(),
							"is_permanent":   fieldBoolean(),
						},
					},

					// Policy
					"policy": map[string]interface{}{
						"properties": map[string]interface{}{
							"policies_applied": fieldKeyword(),
							"policy_action":    fieldKeyword(),
							"policy_score":     fieldFloat(),
						},
					},

					// Error
					"error": map[string]interface{}{
						"properties": map[string]interface{}{
							"message":   fieldText(),
							"code":      fieldKeyword(),
							"category":  fieldKeyword(),
							"retryable": fieldBoolean(),
						},
					},

					// Headers (flexible object for dynamic header names)
					"headers": map[string]interface{}{
						"type":    "object",
						"enabled": true,
					},
				},
			},
		},
		"priority": 100,
		"version":  1,
		"_meta": map[string]interface{}{
			"description": "Mail events index template for email service",
			"created_by":  "go-emailservice-ads",
		},
	}

	// Marshal to JSON
	body, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	// Create template
	res, err := c.es.Indices.PutIndexTemplate(
		templateName,
		bytes.NewReader(body),
		c.es.Indices.PutIndexTemplate.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch error creating template: %s", res.String())
	}

	c.logger.Info("Index template created",
		zap.String("template", templateName),
		zap.Int("shards", c.config.Elasticsearch.Shards),
		zap.Int("replicas", c.config.Elasticsearch.Replicas))

	return nil
}

// CreateILMPolicy creates an Index Lifecycle Management policy for mail events
func (c *Client) CreateILMPolicy(ctx context.Context) error {
	policyName := fmt.Sprintf("%s-ilm-policy", c.indexPrefix)
	retentionDays := c.config.Elasticsearch.RetentionDays

	policy := map[string]interface{}{
		"policy": map[string]interface{}{
			"phases": map[string]interface{}{
				"hot": map[string]interface{}{
					"min_age": "0ms",
					"actions": map[string]interface{}{
						"rollover": map[string]interface{}{
							"max_age":  "1d",
							"max_size": "50gb",
						},
						"set_priority": map[string]interface{}{
							"priority": 100,
						},
					},
				},
				"warm": map[string]interface{}{
					"min_age": "7d",
					"actions": map[string]interface{}{
						"shrink": map[string]interface{}{
							"number_of_shards": 1,
						},
						"forcemerge": map[string]interface{}{
							"max_num_segments": 1,
						},
						"set_priority": map[string]interface{}{
							"priority": 50,
						},
					},
				},
				"delete": map[string]interface{}{
					"min_age": fmt.Sprintf("%dd", retentionDays),
					"actions": map[string]interface{}{
						"delete": map[string]interface{}{},
					},
				},
			},
		},
	}

	// Marshal to JSON
	body, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal ILM policy: %w", err)
	}

	// Create policy
	res, err := c.es.ILM.PutLifecycle(
		policyName,
		c.es.ILM.PutLifecycle.WithBody(bytes.NewReader(body)),
		c.es.ILM.PutLifecycle.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create ILM policy: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch error creating ILM policy: %s", res.String())
	}

	c.logger.Info("ILM policy created",
		zap.String("policy", policyName),
		zap.Int("retention_days", retentionDays))

	return nil
}

// EnsureIndex ensures the current day's index exists
func (c *Client) EnsureIndex(ctx context.Context) error {
	indexName := c.GetIndexName()

	// Check if index exists
	res, err := c.es.Indices.Exists([]string{indexName})
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Index already exists
	if res.StatusCode == 200 {
		return nil
	}

	// Create index (template will apply automatically)
	createRes, err := c.es.Indices.Create(indexName)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer createRes.Body.Close()

	if createRes.IsError() {
		// Ignore "already exists" errors
		if !strings.Contains(createRes.String(), "resource_already_exists") {
			return fmt.Errorf("elasticsearch error creating index: %s", createRes.String())
		}
	}

	c.logger.Info("Index created", zap.String("index", indexName))
	return nil
}

// Field type helpers

func fieldKeyword() map[string]interface{} {
	return map[string]interface{}{
		"type": "keyword",
	}
}

func fieldText() map[string]interface{} {
	return map[string]interface{}{
		"type": "text",
	}
}

func fieldEmail() map[string]interface{} {
	return map[string]interface{}{
		"type":     "text",
		"analyzer": "email_analyzer",
		"fields": map[string]interface{}{
			"keyword": map[string]interface{}{
				"type": "keyword",
			},
		},
	}
}

func fieldDate() map[string]interface{} {
	return map[string]interface{}{
		"type": "date",
	}
}

func fieldIP() map[string]interface{} {
	return map[string]interface{}{
		"type": "ip",
	}
}

func fieldLong() map[string]interface{} {
	return map[string]interface{}{
		"type": "long",
	}
}

func fieldInteger() map[string]interface{} {
	return map[string]interface{}{
		"type": "integer",
	}
}

func fieldFloat() map[string]interface{} {
	return map[string]interface{}{
		"type": "float",
	}
}

func fieldBoolean() map[string]interface{} {
	return map[string]interface{}{
		"type": "boolean",
	}
}
