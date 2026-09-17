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

	model, _ := getResponseHeader(headers, liteLLMModelNameHeader)
	model = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), liteLLMFireworksModelPrefix))
	return &liteLLMResponseMetadata{cost: cost, model: model}
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
	if response.Usage == nil {
		response.Usage = &schemas.BifrostLLMUsage{}
	}
	if response.Usage.Cost == nil {
		response.Usage.Cost = metadata.cost
	}
	applyLiteLLMServedModel(&response.Model, &response.ExtraFields, metadata.model)
}

func applyLiteLLMResponsesResponseMetadata(response *schemas.BifrostResponsesResponse, metadata *liteLLMResponseMetadata) {
	if response == nil || metadata == nil {
		return
	}
	if response.Usage == nil {
		response.Usage = &schemas.ResponsesResponseUsage{}
	}
	if response.Usage.Cost == nil {
		response.Usage.Cost = metadata.cost
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
