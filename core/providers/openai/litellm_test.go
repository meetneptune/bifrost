package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestApplyLiteLLMChatResponseMetadata(t *testing.T) {
	t.Run("full cost breakdown and Fireworks model", func(t *testing.T) {
		response := &schemas.BifrostChatResponse{
			Model: "accounts/fireworks/routers/firerouter",
			Usage: &schemas.BifrostLLMUsage{},
			ExtraFields: schemas.BifrostResponseExtraFields{
				OriginalModelRequested: "accounts/fireworks/routers/firerouter",
			},
		}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Litellm-Response-Cost":           "0.0001132",
			"X-Litellm-Response-Cost-Input":     "0.0000600",
			"X-Litellm-Response-Cost-Output":    "0.0000532",
			"X-Litellm-Response-Cost-Reasoning": "0.0000100",
			"X-Litellm-Response-Cost-Cache":     "0.0000020",
			"X-Litellm-Model-Name":              "fireworks_ai/accounts/fireworks/models/glm-5p3",
		})

		applyLiteLLMChatResponseMetadata(response, metadata)

		if response.Usage.Cost == nil {
			t.Fatal("expected provider-reported cost")
		}
		cost := response.Usage.Cost
		if cost.TotalCost != 0.0001132 || cost.InputCost != 0.0000600 || cost.OutputCost != 0.0000532 {
			t.Fatalf("unexpected top-level cost: %#v", cost)
		}
		if cost.InputCostDetails == nil || cost.InputCostDetails.TextCost != 0.0000580 || cost.InputCostDetails.CachedReadCost != 0.0000020 {
			t.Fatalf("unexpected input cost details: %#v", cost.InputCostDetails)
		}
		if cost.OutputCostDetails == nil || cost.OutputCostDetails.TextCost != 0.0000432 || cost.OutputCostDetails.ReasoningCost != 0.0000100 {
			t.Fatalf("unexpected output cost details: %#v", cost.OutputCostDetails)
		}
		wantModel := "accounts/fireworks/models/glm-5p3"
		if response.Model != wantModel {
			t.Fatalf("model = %q, want %q", response.Model, wantModel)
		}
		if response.ExtraFields.RoutingInfo.ServerSideFallbackModel == nil || *response.ExtraFields.RoutingInfo.ServerSideFallbackModel != wantModel {
			t.Fatalf("served model = %#v, want %q", response.ExtraFields.RoutingInfo.ServerSideFallbackModel, wantModel)
		}
		if response.ExtraFields.OriginalModelRequested != "accounts/fireworks/routers/firerouter" {
			t.Fatalf("original model changed to %q", response.ExtraFields.OriginalModelRequested)
		}
	})

	t.Run("total only", func(t *testing.T) {
		response := &schemas.BifrostChatResponse{Usage: &schemas.BifrostLLMUsage{}}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"x-litellm-response-cost": "0.25",
		})
		applyLiteLLMChatResponseMetadata(response, metadata)
		if response.Usage.Cost == nil || response.Usage.Cost.TotalCost != 0.25 {
			t.Fatalf("cost = %#v, want total 0.25", response.Usage.Cost)
		}
		if response.Usage.Cost.InputCostDetails != nil || response.Usage.Cost.OutputCostDetails != nil {
			t.Fatalf("unexpected breakdown: %#v", response.Usage.Cost)
		}
	})

	t.Run("malformed breakdown field is ignored", func(t *testing.T) {
		response := &schemas.BifrostChatResponse{Usage: &schemas.BifrostLLMUsage{}}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Litellm-Response-Cost":        "0.25",
			"X-Litellm-Response-Cost-Input":  "not-a-number",
			"X-Litellm-Response-Cost-Output": "0.10",
		})
		applyLiteLLMChatResponseMetadata(response, metadata)
		if response.Usage.Cost == nil || response.Usage.Cost.TotalCost != 0.25 || response.Usage.Cost.OutputCost != 0.10 {
			t.Fatalf("unexpected valid cost fields: %#v", response.Usage.Cost)
		}
		if response.Usage.Cost.InputCostDetails != nil {
			t.Fatalf("malformed input cost was applied: %#v", response.Usage.Cost.InputCostDetails)
		}
	})

	t.Run("absent header is byte-for-byte inert", func(t *testing.T) {
		response := &schemas.BifrostChatResponse{
			Model: "gpt-4o",
			Usage: &schemas.BifrostLLMUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
		}
		before, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		applyLiteLLMChatResponseMetadata(response, parseLiteLLMResponseMetadata(map[string]string{"X-Other": "value"}))
		after, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("response changed without LiteLLM cost header:\nbefore %s\nafter  %s", before, after)
		}
	})

	t.Run("malformed total leaves cost alone but still applies the served model", func(t *testing.T) {
		existing := &schemas.BifrostCost{TotalCost: 0.75}
		response := &schemas.BifrostChatResponse{
			Model: "router",
			Usage: &schemas.BifrostLLMUsage{Cost: existing},
		}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Litellm-Response-Cost": "not-a-number",
			"X-Litellm-Model-Name":    "fireworks_ai/accounts/fireworks/models/glm-5p3",
		})
		applyLiteLLMChatResponseMetadata(response, metadata)
		if response.Usage.Cost != existing {
			t.Fatalf("malformed total changed cost: %#v", response.Usage.Cost)
		}
		if response.Model != "accounts/fireworks/models/glm-5p3" {
			t.Fatalf("served model was discarded with the malformed cost: %q", response.Model)
		}
	})

	// A streaming response carries the served model but no cost: LiteLLM
	// flushes headers before the first token, so it cannot know the cost yet.
	// The model must survive, because it is what re-keys pricing onto the
	// backend that actually ran.
	t.Run("absent cost still applies the served model and re-keys pricing", func(t *testing.T) {
		response := &schemas.BifrostChatResponse{Model: "accounts/fireworks/routers/firerouter"}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Litellm-Model-Name":           "fireworks_ai/accounts/fireworks/models/glm-5p3-flash",
			"X-Litellm-Response-Cost-Input":  "0.0",
			"X-Litellm-Response-Cost-Output": "0.0",
			"X-Litellm-Key-Spend":            "0.0",
		})
		if metadata == nil {
			t.Fatal("metadata was dropped even though the served model was present")
		}
		if metadata.cost != nil {
			t.Fatalf("a cost was invented from zero-valued streaming headers: %#v", metadata.cost)
		}
		applyLiteLLMChatResponseMetadata(response, metadata)
		if response.Model != "accounts/fireworks/models/glm-5p3-flash" {
			t.Fatalf("served model not applied: %q", response.Model)
		}
		served := response.ExtraFields.RoutingInfo.ServerSideFallbackModel
		if served == nil || *served != "accounts/fireworks/models/glm-5p3-flash" {
			t.Fatalf("pricing was not re-keyed onto the served model: %v", served)
		}
		if response.Usage != nil && response.Usage.Cost != nil {
			t.Fatalf("a zero cost was attached where none was reported: %#v", response.Usage.Cost)
		}
	})

	t.Run("no cost and no model yields no metadata", func(t *testing.T) {
		if metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Request-Id": "abc",
		}); metadata != nil {
			t.Fatalf("metadata synthesized from unrelated headers: %#v", metadata)
		}
	})

	t.Run("existing neutral cost is not overwritten", func(t *testing.T) {
		existing := &schemas.BifrostCost{TotalCost: 0.75}
		response := &schemas.BifrostChatResponse{
			Model: "router",
			Usage: &schemas.BifrostLLMUsage{Cost: existing},
		}
		metadata := parseLiteLLMResponseMetadata(map[string]string{
			"X-Litellm-Response-Cost": "0.25",
			"X-Litellm-Model-Name":    "fireworks_ai/accounts/fireworks/models/glm-5p3",
		})
		applyLiteLLMChatResponseMetadata(response, metadata)
		if response.Usage.Cost != existing || response.Usage.Cost.TotalCost != 0.75 {
			t.Fatalf("existing cost was overwritten: %#v", response.Usage.Cost)
		}
		if response.Model != "accounts/fireworks/models/glm-5p3" {
			t.Fatalf("served model = %q", response.Model)
		}
	})
}

func TestApplyLiteLLMResponsesResponseMetadata(t *testing.T) {
	response := &schemas.BifrostResponsesResponse{
		Model: "accounts/fireworks/routers/firerouter",
		Usage: &schemas.ResponsesResponseUsage{},
	}
	metadata := parseLiteLLMResponseMetadata(map[string]string{
		"X-Litellm-Response-Cost": "0.125",
		"X-Litellm-Model-Name":    "fireworks_ai/accounts/fireworks/models/glm-5p3",
	})
	applyLiteLLMResponsesResponseMetadata(response, metadata)

	if response.Usage.Cost == nil || response.Usage.Cost.TotalCost != 0.125 {
		t.Fatalf("cost = %#v, want total 0.125", response.Usage.Cost)
	}
	if response.Model != "accounts/fireworks/models/glm-5p3" {
		t.Fatalf("model = %q", response.Model)
	}
}
