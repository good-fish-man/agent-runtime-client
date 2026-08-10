package infrapkg

import "testing"

type typedSuggestion struct {
	ID         string         `json:"id"`
	Capability string         `json:"capability"`
	Arguments  map[string]any `json:"arguments"`
}

func TestToStructNormalizesTypedDeviceObservation(t *testing.T) {
	value := ToStruct(map[string]any{
		"latest_action_observation": map[string]any{
			"status": "SUCCEEDED",
			"state": map[string]any{
				"suggested_actions": []typedSuggestion{{
					ID: "option-1", Capability: "browser.play", Arguments: map[string]any{"snapshot": true},
				}},
			},
		},
	})
	if value == nil {
		t.Fatal("typed observation context was dropped")
	}
	state := value.AsMap()["latest_action_observation"].(map[string]any)["state"].(map[string]any)
	actions := state["suggested_actions"].([]any)
	if len(actions) != 1 || actions[0].(map[string]any)["capability"] != "browser.play" {
		t.Fatalf("unexpected normalized actions: %#v", actions)
	}
}
