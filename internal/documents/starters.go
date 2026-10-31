package documents

// Starter is a template to start from (the "New template" window).
type Starter struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Spec        Spec   `json:"spec"`
}

func starters() []Starter {
	footer := []Block{
		{Type: "divider"},
		{Type: "text", Text: "{{app.name}} · Page {{page}} / {{pages}}", Align: "center", Size: 8, Color: "#6b7280"},
	}
	return []Starter{
		{
			Key: "invoice", Name: "Invoice", Icon: "ri-bill-line",
			Description: "Your details and the customer's, the lines of a list, the totals.",
			Spec: Spec{
				Format: "a4", Orientation: "portrait", FontSize: 10, Accent: "#2563eb",
				Margins:  Margins{Top: 18, Right: 18, Bottom: 18, Left: 18},
				Filename: "invoice-{{record.id}}",
				Header: []Block{{Type: "columns", Columns: []Column{
					{Width: 60, Blocks: []Block{{Type: "text", Text: "**{{app.name}}**", Size: 14}}},
					{Width: 40, Blocks: []Block{{Type: "text", Text: "{{app.url}}", Align: "right", Size: 8, Color: "#6b7280"}}},
				}}},
				Body: []Block{
					{Type: "heading", Level: 1, Text: "Invoice"},
					{Type: "columns", Columns: []Column{
						{Width: 50, Blocks: []Block{
							{Type: "text", Text: "**Billed to**"},
							{Type: "text", Text: "{{record}}"},
						}},
						{Width: 50, Blocks: []Block{{Type: "fields", LabelWidth: 45, Rows: []Row{
							{Label: "Invoice no.", Value: "{{record.id}}"},
							{Label: "Date", Value: `{{formatDate "DD/MM/YYYY" now}}`},
							{Label: "Due", Value: `{{formatDate "DD/MM/YYYY" (addDays 30 now)}}`},
						}}}},
					}},
					{Type: "spacer", Height: 4},
					{Type: "table", Items: "{{lists.items}}", Zebra: true, Empty: "Add a list named “items” (Data) for the lines.",
						Cells: []TableCol{
							{Header: "Description", Value: "{{row}}", Width: 55},
							{Header: "Qty", Value: "{{row.quantity}}", Width: 10, Align: "right"},
							{Header: "Unit price", Value: `{{currency "EUR" row.price}}`, Width: 17, Align: "right"},
							{Header: "Amount", Value: `{{currency "EUR" (multiply row.price row.quantity)}}`, Width: 18, Align: "right"},
						},
						Totals: []Row{
							{Label: "Total", Value: `{{currency "EUR" record.total}}`},
						}},
					{Type: "spacer", Height: 6},
					{Type: "text", Text: "Thank you for your business.", Color: "#6b7280"},
				},
				Footer: footer,
			},
		},
		{
			Key: "quote", Name: "Quote", Icon: "ri-file-list-3-line",
			Description: "A priced proposal, valid for a number of days.",
			Spec: Spec{
				Format: "a4", Orientation: "portrait", FontSize: 10, Accent: "#0f766e",
				Margins:  Margins{Top: 18, Right: 18, Bottom: 18, Left: 18},
				Filename: "quote-{{record.id}}",
				Body: []Block{
					{Type: "heading", Level: 1, Text: "Quote"},
					{Type: "fields", Rows: []Row{
						{Label: "For", Value: "{{record}}"},
						{Label: "Date", Value: `{{formatDate "DD/MM/YYYY" now}}`},
						{Label: "Valid until", Value: `{{formatDate "DD/MM/YYYY" (addDays 30 now)}}`},
					}},
					{Type: "spacer", Height: 4},
					{Type: "table", Items: "{{lists.items}}", Empty: "Add a list named “items” (Data) for the lines.",
						Cells: []TableCol{
							{Header: "Item", Value: "{{row}}", Width: 70},
							{Header: "Price", Value: `{{currency "EUR" row.price}}`, Width: 30, Align: "right"},
						}},
					{Type: "text", Text: "_Prices excluding taxes._", Size: 8, Color: "#6b7280"},
				},
				Footer: footer,
			},
		},
		{
			Key: "certificate", Name: "Certificate", Icon: "ri-award-line",
			Description: "A centered, landscape page with a name and a date.",
			Spec: Spec{
				Format: "a4", Orientation: "landscape", FontSize: 12, Accent: "#b45309",
				Margins:  Margins{Top: 30, Right: 30, Bottom: 30, Left: 30},
				Filename: "certificate-{{record.id}}",
				Body: []Block{
					{Type: "spacer", Height: 20},
					{Type: "heading", Level: 1, Text: "Certificate", Align: "center", Size: 36},
					{Type: "text", Text: "This certifies that", Align: "center", Color: "#6b7280"},
					{Type: "spacer", Height: 4},
					{Type: "heading", Level: 2, Text: "{{record}}", Align: "center", Size: 26},
					{Type: "spacer", Height: 4},
					{Type: "text", Text: "has completed the program on " + `{{formatDate "DD MMMM YYYY" now}}` + ".", Align: "center"},
					{Type: "spacer", Height: 24},
					{Type: "divider"},
					{Type: "text", Text: "{{app.name}}", Align: "center", Size: 9, Color: "#6b7280"},
				},
			},
		},
		{
			Key: "blank", Name: "Blank", Icon: "ri-file-line",
			Description: "A title and a text, to build your own.",
			Spec: Spec{
				Format: "a4", Orientation: "portrait", FontSize: 10, Accent: "#2563eb",
				Margins: Margins{Top: 18, Right: 18, Bottom: 18, Left: 18},
				Body: []Block{
					{Type: "heading", Level: 1, Text: "Title"},
					{Type: "text", Text: "Write here. **Bold**, *italic* and [links](https://example.com) work, and formulas: {{app.name}}."},
				},
				Footer: footer,
			},
		},
	}
}
