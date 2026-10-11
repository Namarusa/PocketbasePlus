package pages

import (
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Chart colors: the --chart-1..5 colors of the charts. The palettes are the
// chart colors of the shadcn/ui themes (MIT, github.com/shadcn-ui/ui,
// apps/v4/registry/themes.ts), "multicolor" is shadcn/ui's classic default.
// A setting is "" (the style's colors), a palette name, "primary" (shades of
// the primary color) or "custom:c1,c2,c3,c4,c5".

type palette struct {
	light, dark [5]string
}

var chartPalettes = map[string]palette{
	"multicolor": {
		light: [5]string{"oklch(0.646 0.222 41.116)", "oklch(0.6 0.118 184.704)", "oklch(0.398 0.07 227.392)", "oklch(0.828 0.189 84.429)", "oklch(0.769 0.188 70.08)"},
		dark:  [5]string{"oklch(0.488 0.243 264.376)", "oklch(0.696 0.17 162.48)", "oklch(0.769 0.188 70.08)", "oklch(0.627 0.265 303.9)", "oklch(0.645 0.246 16.439)"},
	},
	"neutral": {
		light: [5]string{"oklch(0.87 0 0)", "oklch(0.556 0 0)", "oklch(0.439 0 0)", "oklch(0.371 0 0)", "oklch(0.269 0 0)"},
		dark:  [5]string{"oklch(0.87 0 0)", "oklch(0.556 0 0)", "oklch(0.439 0 0)", "oklch(0.371 0 0)", "oklch(0.269 0 0)"},
	},
	"stone": {
		light: [5]string{"oklch(0.869 0.005 56.366)", "oklch(0.553 0.013 58.071)", "oklch(0.444 0.011 73.639)", "oklch(0.374 0.01 67.558)", "oklch(0.268 0.007 34.298)"},
		dark:  [5]string{"oklch(0.869 0.005 56.366)", "oklch(0.553 0.013 58.071)", "oklch(0.444 0.011 73.639)", "oklch(0.374 0.01 67.558)", "oklch(0.268 0.007 34.298)"},
	},
	"zinc": {
		light: [5]string{"oklch(0.871 0.006 286.286)", "oklch(0.552 0.016 285.938)", "oklch(0.442 0.017 285.786)", "oklch(0.37 0.013 285.805)", "oklch(0.274 0.006 286.033)"},
		dark:  [5]string{"oklch(0.871 0.006 286.286)", "oklch(0.552 0.016 285.938)", "oklch(0.442 0.017 285.786)", "oklch(0.37 0.013 285.805)", "oklch(0.274 0.006 286.033)"},
	},
	"mauve": {
		light: [5]string{"oklch(0.865 0.012 325.68)", "oklch(0.542 0.034 322.5)", "oklch(0.435 0.029 321.78)", "oklch(0.364 0.029 323.89)", "oklch(0.263 0.024 320.12)"},
		dark:  [5]string{"oklch(0.865 0.012 325.68)", "oklch(0.542 0.034 322.5)", "oklch(0.435 0.029 321.78)", "oklch(0.364 0.029 323.89)", "oklch(0.263 0.024 320.12)"},
	},
	"olive": {
		light: [5]string{"oklch(0.88 0.011 106.6)", "oklch(0.58 0.031 107.3)", "oklch(0.466 0.025 107.3)", "oklch(0.394 0.023 107.4)", "oklch(0.286 0.016 107.4)"},
		dark:  [5]string{"oklch(0.88 0.011 106.6)", "oklch(0.58 0.031 107.3)", "oklch(0.466 0.025 107.3)", "oklch(0.394 0.023 107.4)", "oklch(0.286 0.016 107.4)"},
	},
	"mist": {
		light: [5]string{"oklch(0.872 0.007 219.6)", "oklch(0.56 0.021 213.5)", "oklch(0.45 0.017 213.2)", "oklch(0.378 0.015 216)", "oklch(0.275 0.011 216.9)"},
		dark:  [5]string{"oklch(0.872 0.007 219.6)", "oklch(0.56 0.021 213.5)", "oklch(0.45 0.017 213.2)", "oklch(0.378 0.015 216)", "oklch(0.275 0.011 216.9)"},
	},
	"taupe": {
		light: [5]string{"oklch(0.868 0.007 39.5)", "oklch(0.547 0.021 43.1)", "oklch(0.438 0.017 39.3)", "oklch(0.367 0.016 35.7)", "oklch(0.268 0.011 36.5)"},
		dark:  [5]string{"oklch(0.868 0.007 39.5)", "oklch(0.547 0.021 43.1)", "oklch(0.438 0.017 39.3)", "oklch(0.367 0.016 35.7)", "oklch(0.268 0.011 36.5)"},
	},
	"amber": {
		light: [5]string{"oklch(0.879 0.169 91.605)", "oklch(0.769 0.188 70.08)", "oklch(0.666 0.179 58.318)", "oklch(0.555 0.163 48.998)", "oklch(0.473 0.137 46.201)"},
		dark:  [5]string{"oklch(0.879 0.169 91.605)", "oklch(0.769 0.188 70.08)", "oklch(0.666 0.179 58.318)", "oklch(0.555 0.163 48.998)", "oklch(0.473 0.137 46.201)"},
	},
	"blue": {
		light: [5]string{"oklch(0.809 0.105 251.813)", "oklch(0.623 0.214 259.815)", "oklch(0.546 0.245 262.881)", "oklch(0.488 0.243 264.376)", "oklch(0.424 0.199 265.638)"},
		dark:  [5]string{"oklch(0.809 0.105 251.813)", "oklch(0.623 0.214 259.815)", "oklch(0.546 0.245 262.881)", "oklch(0.488 0.243 264.376)", "oklch(0.424 0.199 265.638)"},
	},
	"cyan": {
		light: [5]string{"oklch(0.865 0.127 207.078)", "oklch(0.715 0.143 215.221)", "oklch(0.609 0.126 221.723)", "oklch(0.52 0.105 223.128)", "oklch(0.45 0.085 224.283)"},
		dark:  [5]string{"oklch(0.865 0.127 207.078)", "oklch(0.715 0.143 215.221)", "oklch(0.609 0.126 221.723)", "oklch(0.52 0.105 223.128)", "oklch(0.45 0.085 224.283)"},
	},
	"emerald": {
		light: [5]string{"oklch(0.845 0.143 164.978)", "oklch(0.696 0.17 162.48)", "oklch(0.596 0.145 163.225)", "oklch(0.508 0.118 165.612)", "oklch(0.432 0.095 166.913)"},
		dark:  [5]string{"oklch(0.845 0.143 164.978)", "oklch(0.696 0.17 162.48)", "oklch(0.596 0.145 163.225)", "oklch(0.508 0.118 165.612)", "oklch(0.432 0.095 166.913)"},
	},
	"fuchsia": {
		light: [5]string{"oklch(0.833 0.145 321.434)", "oklch(0.667 0.295 322.15)", "oklch(0.591 0.293 322.896)", "oklch(0.518 0.253 323.949)", "oklch(0.452 0.211 324.591)"},
		dark:  [5]string{"oklch(0.833 0.145 321.434)", "oklch(0.667 0.295 322.15)", "oklch(0.591 0.293 322.896)", "oklch(0.518 0.253 323.949)", "oklch(0.452 0.211 324.591)"},
	},
	"green": {
		light: [5]string{"oklch(0.871 0.15 154.449)", "oklch(0.723 0.219 149.579)", "oklch(0.627 0.194 149.214)", "oklch(0.527 0.154 150.069)", "oklch(0.448 0.119 151.328)"},
		dark:  [5]string{"oklch(0.871 0.15 154.449)", "oklch(0.723 0.219 149.579)", "oklch(0.627 0.194 149.214)", "oklch(0.527 0.154 150.069)", "oklch(0.448 0.119 151.328)"},
	},
	"indigo": {
		light: [5]string{"oklch(0.785 0.115 274.713)", "oklch(0.585 0.233 277.117)", "oklch(0.511 0.262 276.966)", "oklch(0.457 0.24 277.023)", "oklch(0.398 0.195 277.366)"},
		dark:  [5]string{"oklch(0.785 0.115 274.713)", "oklch(0.585 0.233 277.117)", "oklch(0.511 0.262 276.966)", "oklch(0.457 0.24 277.023)", "oklch(0.398 0.195 277.366)"},
	},
	"lime": {
		light: [5]string{"oklch(0.897 0.196 126.665)", "oklch(0.768 0.233 130.85)", "oklch(0.648 0.2 131.684)", "oklch(0.532 0.157 131.589)", "oklch(0.453 0.124 130.933)"},
		dark:  [5]string{"oklch(0.897 0.196 126.665)", "oklch(0.768 0.233 130.85)", "oklch(0.648 0.2 131.684)", "oklch(0.532 0.157 131.589)", "oklch(0.453 0.124 130.933)"},
	},
	"orange": {
		light: [5]string{"oklch(0.837 0.128 66.29)", "oklch(0.705 0.213 47.604)", "oklch(0.646 0.222 41.116)", "oklch(0.553 0.195 38.402)", "oklch(0.47 0.157 37.304)"},
		dark:  [5]string{"oklch(0.837 0.128 66.29)", "oklch(0.705 0.213 47.604)", "oklch(0.646 0.222 41.116)", "oklch(0.553 0.195 38.402)", "oklch(0.47 0.157 37.304)"},
	},
	"pink": {
		light: [5]string{"oklch(0.823 0.12 346.018)", "oklch(0.656 0.241 354.308)", "oklch(0.592 0.249 0.584)", "oklch(0.525 0.223 3.958)", "oklch(0.459 0.187 3.815)"},
		dark:  [5]string{"oklch(0.823 0.12 346.018)", "oklch(0.656 0.241 354.308)", "oklch(0.592 0.249 0.584)", "oklch(0.525 0.223 3.958)", "oklch(0.459 0.187 3.815)"},
	},
	"purple": {
		light: [5]string{"oklch(0.827 0.119 306.383)", "oklch(0.627 0.265 303.9)", "oklch(0.558 0.288 302.321)", "oklch(0.496 0.265 301.924)", "oklch(0.438 0.218 303.724)"},
		dark:  [5]string{"oklch(0.827 0.119 306.383)", "oklch(0.627 0.265 303.9)", "oklch(0.558 0.288 302.321)", "oklch(0.496 0.265 301.924)", "oklch(0.438 0.218 303.724)"},
	},
	"red": {
		light: [5]string{"oklch(0.808 0.114 19.571)", "oklch(0.637 0.237 25.331)", "oklch(0.577 0.245 27.325)", "oklch(0.505 0.213 27.518)", "oklch(0.444 0.177 26.899)"},
		dark:  [5]string{"oklch(0.808 0.114 19.571)", "oklch(0.637 0.237 25.331)", "oklch(0.577 0.245 27.325)", "oklch(0.505 0.213 27.518)", "oklch(0.444 0.177 26.899)"},
	},
	"rose": {
		light: [5]string{"oklch(0.81 0.117 11.638)", "oklch(0.645 0.246 16.439)", "oklch(0.586 0.253 17.585)", "oklch(0.514 0.222 16.935)", "oklch(0.455 0.188 13.697)"},
		dark:  [5]string{"oklch(0.81 0.117 11.638)", "oklch(0.645 0.246 16.439)", "oklch(0.586 0.253 17.585)", "oklch(0.514 0.222 16.935)", "oklch(0.455 0.188 13.697)"},
	},
	"sky": {
		light: [5]string{"oklch(0.828 0.111 230.318)", "oklch(0.685 0.169 237.323)", "oklch(0.588 0.158 241.966)", "oklch(0.5 0.134 242.749)", "oklch(0.443 0.11 240.79)"},
		dark:  [5]string{"oklch(0.828 0.111 230.318)", "oklch(0.685 0.169 237.323)", "oklch(0.588 0.158 241.966)", "oklch(0.5 0.134 242.749)", "oklch(0.443 0.11 240.79)"},
	},
	"teal": {
		light: [5]string{"oklch(0.855 0.138 181.071)", "oklch(0.704 0.14 182.503)", "oklch(0.6 0.118 184.704)", "oklch(0.511 0.096 186.391)", "oklch(0.437 0.078 188.216)"},
		dark:  [5]string{"oklch(0.855 0.138 181.071)", "oklch(0.704 0.14 182.503)", "oklch(0.6 0.118 184.704)", "oklch(0.511 0.096 186.391)", "oklch(0.437 0.078 188.216)"},
	},
	"violet": {
		light: [5]string{"oklch(0.811 0.111 293.571)", "oklch(0.606 0.25 292.717)", "oklch(0.541 0.281 293.009)", "oklch(0.491 0.27 292.581)", "oklch(0.432 0.232 292.759)"},
		dark:  [5]string{"oklch(0.811 0.111 293.571)", "oklch(0.606 0.25 292.717)", "oklch(0.541 0.281 293.009)", "oklch(0.491 0.27 292.581)", "oklch(0.432 0.232 292.759)"},
	},
	"yellow": {
		light: [5]string{"oklch(0.905 0.182 98.111)", "oklch(0.795 0.184 86.047)", "oklch(0.681 0.162 75.834)", "oklch(0.554 0.135 66.442)", "oklch(0.476 0.114 61.907)"},
		dark:  [5]string{"oklch(0.905 0.182 98.111)", "oklch(0.795 0.184 86.047)", "oklch(0.681 0.162 75.834)", "oklch(0.554 0.135 66.442)", "oklch(0.476 0.114 61.907)"},
	},
}

// paletteOrder is the order of the pickers.
var paletteOrder = []string{"multicolor", "neutral", "stone", "zinc", "mauve", "olive", "mist", "taupe", "amber", "blue", "cyan", "emerald", "fuchsia", "green", "indigo", "lime", "orange", "pink", "purple", "red", "rose", "sky", "teal", "violet", "yellow"}

// chartColors returns the light and dark colors of a setting, or ok false for
// "" or an invalid setting.
func chartColors(v string) (light, dark [5]string, ok bool) {
	v = strings.TrimSpace(v)
	if p, found := chartPalettes[v]; found {
		return p.light, p.dark, true
	}
	if v == "primary" {
		for i, pct := range []int{100, 75, 55, 40, 25} {
			c := "color-mix(in oklab, var(--primary) " + strconv.Itoa(pct) + "%, var(--background))"
			if pct == 100 {
				c = "var(--primary)"
			}
			light[i], dark[i] = c, c
		}
		return light, dark, true
	}
	list, found := strings.CutPrefix(v, "custom:")
	if !found {
		return light, dark, false
	}
	colors := strings.Split(list, ",")
	if len(colors) == 0 || len(colors) > 5 {
		return light, dark, false
	}
	for i := 0; i < 5; i++ {
		c := strings.TrimSpace(colors[i%len(colors)])
		if !hexRegex.MatchString(c) {
			return light, dark, false
		}
		light[i], dark[i] = c, c
	}
	return light, dark, true
}

// chartColorsCSS is the theme CSS of the chart colors setting.
func chartColorsCSS(v string) string {
	light, dark, ok := chartColors(v)
	if !ok {
		return ""
	}
	vars := func(c [5]string) string {
		var b strings.Builder
		for i, x := range c {
			b.WriteString("--chart-" + strconv.Itoa(i+1) + ":" + x + ";")
		}
		return b.String()
	}
	return ":root{" + vars(light) + "}\n.dark{" + vars(dark) + "}\n"
}

// chartPalettesRoute lists the palettes for the pickers of the dashboard.
func chartPalettesRoute(e *core.RequestEvent) error {
	type entry struct {
		Name  string    `json:"name"`
		Light [5]string `json:"light"`
		Dark  [5]string `json:"dark"`
	}
	out := make([]entry, 0, len(paletteOrder))
	for _, n := range paletteOrder {
		p := chartPalettes[n]
		out = append(out, entry{n, p.light, p.dark})
	}
	return e.JSON(200, out)
}
