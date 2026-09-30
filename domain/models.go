package domain

type AttributionMetadata struct {
	SystemPrompt   string         `json:"system_prompt,omitempty"`
	DefaultModel   string         `json:"default_model,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type TeamMetadata struct {
	SystemPrompt   string         `json:"system_prompt,omitempty"`
	DefaultModel   string         `json:"default_model,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type RequestMetadata struct {
	TeamMetadata *TeamMetadata `json:"user_api_key_team_metadata,omitempty"`
	AuthMetadata *TeamMetadata `json:"user_api_key_auth_metadata,omitempty"`
}

func (m *RequestMetadata) resolve() AttributionMetadata {
	merged := TeamMetadata{}

	if m != nil && m.TeamMetadata != nil {
		merged.SystemPrompt = m.TeamMetadata.SystemPrompt
		merged.DefaultModel = m.TeamMetadata.DefaultModel
		merged.ResponseFormat = m.TeamMetadata.ResponseFormat
	}

	if m != nil && m.AuthMetadata != nil {
		if m.AuthMetadata.SystemPrompt != "" {
			merged.SystemPrompt = m.AuthMetadata.SystemPrompt
		}
		if m.AuthMetadata.DefaultModel != "" {
			merged.DefaultModel = m.AuthMetadata.DefaultModel
		}
		if m.AuthMetadata.ResponseFormat != nil {
			merged.ResponseFormat = m.AuthMetadata.ResponseFormat
		}
	}

	return AttributionMetadata{
		SystemPrompt:   merged.SystemPrompt,
		DefaultModel:   merged.DefaultModel,
		ResponseFormat: merged.ResponseFormat,
	}
}
