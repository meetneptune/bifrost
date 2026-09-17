package openai

import (
	"math"
	"strconv"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

const (
	liteLLMResponseCostHeader          = "X-Litellm-Response-Cost"
	liteLLMResponseInputCostHeader     = "X-Litellm-Response-Cost-Input"
	liteLLMResponseOutputCostHeader    = "X-Litellm-Response-Cost-Output"
	liteLLMResponseReasoningCostHeader = "X-Litellm-Response-Cost-Reasoning"
	liteLLMResponseCacheCostHeader     = "X-Litellm-Response-Cost-Cache"
	liteLLMModelNameHeader             = "X-Litellm-Model-Name"
	liteLLMFireworksModelPrefix        = "fireworks_ai/"
)

type liteLLMResponseMetadata struct {
	cost  *schemas.BifrostCost
	model string
}

func parseLiteLLMResponseMetadata(headers map[string]string) *liteLLMResponseMetadata {
	// The served model is read independently of cost. A streaming response
	// carries X-Litellm-Model-Name but no X-Litellm-Response-Cost: headers are
	// flushed before the first token, so LiteLLM cannot know the cost yet and
	// sends every cost field as 0.0 with the total omitted. Returning early on
	// the missing total would discard the served model as well, and that model
	// is what lets pricing key on the backend that actually ran.
	model, _ := getResponseHeader(headers, liteLLMModelNameHeader)
	model = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), liteLLMFireworksModelPrefix))

	cost := parseLiteLLMCost(headers)
	if cost == nil && model == "" {
		return nil
	}
	return &liteLLMResponseMetadata{cost: cost, model: model}
}

// parseLiteLLMCost returns the provider-reported cost, or nil when LiteLLM did
// not report one. Nil means "unknown", never "free".
func parseLiteLLMCost(headers map[string]string) *schemas.BifrostCost {
	totalRaw, ok := getResponseHeader(headers, liteLLMResponseCostHeader)
	if !ok {
		return nil
	}
	total, ok := parseNonNegativeCost(totalRaw)
	if !ok {
		return nil
	}

	cost := &schemas.BifrostCost{TotalCost: total}
	input, hasInput := parseOptionalCostHeader(headers, liteLLMResponseInputCostHeader)
	cache, hasCache := parseOptionalCostHeader(headers, liteLLMResponseCacheCostHeader)
	if hasInput || hasCache {
		if !hasInput {
			input = cache
		}
		cost.InputCost = input
		cost.InputCostDetails = &schemas.InputCostDetails{
			TextCost:       max(input-cache, 0),
			CachedReadCost: cache,
		}
	}

	output, hasOutput := parseOptionalCostHeader(headers, liteLLMResponseOutputCostHeader)
	reasoning, hasReasoning := parseOptionalCostHeader(headers, liteLLMResponseReasoningCostHeader)
	if hasOutput || hasReasoning {
		if !hasOutput {
			output = reasoning
		}
		cost.OutputCost = output
		cost.OutputCostDetails = &schemas.OutputCostDetails{
			TextCost:      max(output-reasoning, 0),
			ReasoningCost: reasoning,
		}
	}

	return cost
}

func parseLiteLLMResponseMetadataFromContext(ctx *schemas.BifrostContext) *liteLLMResponseMetadata {
	if ctx == nil {
		return nil
	}
	headers, _ := ctx.Value(schemas.BifrostContextKeyProviderResponseHeaders).(map[string]string)
	return parseLiteLLMResponseMetadata(headers)
}

func getResponseHeader(headers map[string]string, name string) (string, bool) {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}

func parseOptionalCostHeader(headers map[string]string, name string) (float64, bool) {
	raw, ok := getResponseHeader(headers, name)
	if !ok {
		return 0, false
	}
	return parseNonNegativeCost(raw)
}

func parseNonNegativeCost(raw string) (float64, bool) {
	cost, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return 0, false
	}
	return cost, true
}

func applyLiteLLMChatResponseMetadata(response *schemas.BifrostChatResponse, metadata *liteLLMResponseMetadata) {
	if response == nil || metadata == nil {
		return
	}
	// Only attach a cost LiteLLM actually reported. When it reported none the
	// served model below re-keys pricing onto the backend, and Bifrost prices
	// the tokens itself; fabricating a zero here would suppress that and read
	// as a free call.
	if metadata.cost != nil {
		if response.Usage == nil {
			response.Usage = &schemas.BifrostLLMUsage{}
		}
		if response.Usage.Cost == nil {
			response.Usage.Cost = metadata.cost
		}
	}
	applyLiteLLMServedModel(&response.Model, &response.ExtraFields, metadata.model)
}

func applyLiteLLMResponsesResponseMetadata(response *schemas.BifrostResponsesResponse, metadata *liteLLMResponseMetadata) {
	if response == nil || metadata == nil {
		return
	}
	// See applyLiteLLMChatResponseMetadata: attach only a reported cost, so a
	// stream without one falls through to catalog pricing on the served model.
	if metadata.cost != nil {
		if response.Usage == nil {
			response.Usage = &schemas.ResponsesResponseUsage{}
		}
		if response.Usage.Cost == nil {
			response.Usage.Cost = metadata.cost
		}
	}
	applyLiteLLMServedModel(&response.Model, &response.ExtraFields, metadata.model)
}

func applyLiteLLMServedModel(model *string, extraFields *schemas.BifrostResponseExtraFields, servedModel string) {
	if servedModel == "" {
		return
	}
	*model = servedModel
	if extraFields != nil {
		extraFields.RoutingInfo.ServerSideFallbackModel = schemas.Ptr(servedModel)
	}
}
