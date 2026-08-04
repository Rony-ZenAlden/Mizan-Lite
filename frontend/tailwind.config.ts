import type { Config } from "tailwindcss";

// Colours, radii, shadows, and type sizes map to the CSS variables (design tokens)
// declared in src/index.css, so light/dark switch by swapping variables and no
// component hardcodes a raw value. See PHASE_0_FOUNDATION.md §FE.1.
//
// Spacing is deliberately Tailwind's default: it is already a 4px scale, and adding
// a parallel one would give every component two vocabularies for the same idea.
const color = (name: string) => `rgb(var(--color-${name}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: color("surface"),
        "surface-raised": color("surface-raised"),
        "surface-sunken": color("surface-sunken"),
        border: color("border"),
        "border-strong": color("border-strong"),
        text: color("text"),
        "text-muted": color("text-muted"),
        primary: color("primary"),
        "primary-fg": color("primary-fg"),
        "primary-subtle": color("primary-subtle"),
        danger: color("danger"),
        "danger-fg": color("danger-fg"),
        "danger-subtle": color("danger-subtle"),
        success: color("success"),
        "success-fg": color("success-fg"),
        "success-subtle": color("success-subtle"),
        warning: color("warning"),
        "warning-fg": color("warning-fg"),
        "warning-subtle": color("warning-subtle"),
        info: color("info"),
        "info-fg": color("info-fg"),
        "info-subtle": color("info-subtle"),
        ring: color("ring"),
      },
      borderRadius: {
        sm: "var(--radius-sm)",
        DEFAULT: "var(--radius)",
        lg: "var(--radius-lg)",
      },
      boxShadow: {
        sm: "var(--shadow-sm)",
        DEFAULT: "var(--shadow-md)",
        md: "var(--shadow-md)",
        lg: "var(--shadow-lg)",
      },
      fontSize: {
        xs: ["var(--text-xs)", { lineHeight: "var(--text-xs-lh)" }],
        sm: ["var(--text-sm)", { lineHeight: "var(--text-sm-lh)" }],
        base: ["var(--text-base)", { lineHeight: "var(--text-base-lh)" }],
        lg: ["var(--text-lg)", { lineHeight: "var(--text-lg-lh)" }],
        xl: ["var(--text-xl)", { lineHeight: "var(--text-xl-lh)" }],
        "2xl": ["var(--text-2xl)", { lineHeight: "var(--text-2xl-lh)" }],
      },
      fontFamily: {
        sans: ["var(--font-sans)", "system-ui", "sans-serif"],
      },
    },
  },
  plugins: [],
} satisfies Config;
