import type { Config } from "tailwindcss";

// Every colour is a token from src/index.css, so no component holds a raw value and the whole
// palette changes in one place.
const color = (name: string) => `rgb(var(--color-${name}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: color("surface"),
        "surface-raised": color("surface-raised"),
        border: color("border"),
        text: color("text"),
        "text-muted": color("text-muted"),
        primary: color("primary"),
        "primary-fg": color("primary-fg"),
        danger: color("danger"),
        "danger-subtle": color("danger-subtle"),
        success: color("success"),
        "success-subtle": color("success-subtle"),
        ring: color("ring"),
      },
      fontFamily: {
        sans: "var(--font-sans)",
      },
    },
  },
  plugins: [],
} satisfies Config;
