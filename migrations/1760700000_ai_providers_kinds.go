package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// More AI providers: Anthropic, OpenAI, Gemini, DeepSeek, Kimi and GLM (direct APIs).
var newAIKinds = []string{"anthropic", "openai", "gemini", "deepseek", "kimi", "glm"}

func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("ai_providers")
		if err != nil {
			return err
		}
		f, ok := c.Fields.GetByName("kind").(*core.SelectField)
		if !ok {
			return nil
		}
		for _, k := range newAIKinds {
			if !slices.Contains(f.Values, k) {
				f.Values = append(f.Values, k)
			}
		}
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("ai_providers")
		if err != nil {
			return err
		}
		f, ok := c.Fields.GetByName("kind").(*core.SelectField)
		if !ok {
			return nil
		}
		f.Values = slices.DeleteFunc(f.Values, func(v string) bool { return slices.Contains(newAIKinds, v) })
		return app.Save(c)
	})
}
