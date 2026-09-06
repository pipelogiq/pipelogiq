import type { Config } from "tailwindcss";
import tailwindcssAnimate from "tailwindcss-animate";

export default {
  darkMode: ["class"],
  content: ["./pages/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./app/**/*.{ts,tsx}", "./src/**/*.{ts,tsx}"],
  prefix: "",
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        "border-strong": "hsl(var(--border-strong))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        sidebar: {
          DEFAULT: "hsl(var(--sidebar-background))",
          foreground: "hsl(var(--sidebar-foreground))",
          primary: "hsl(var(--sidebar-primary))",
          "primary-foreground": "hsl(var(--sidebar-primary-foreground))",
          accent: "hsl(var(--sidebar-accent))",
          "accent-foreground": "hsl(var(--sidebar-accent-foreground))",
          border: "hsl(var(--sidebar-border))",
          ring: "hsl(var(--sidebar-ring))",
        },
        status: {
          success: "hsl(var(--status-success))",
          "success-bg": "hsl(var(--status-success-bg))",
          warning: "hsl(var(--status-warning))",
          "warning-bg": "hsl(var(--status-warning-bg))",
          error: "hsl(var(--status-error))",
          "error-bg": "hsl(var(--status-error-bg))",
          running: "hsl(var(--status-running))",
          "running-bg": "hsl(var(--status-running-bg))",
          paused: "hsl(var(--status-paused))",
          "paused-bg": "hsl(var(--status-paused-bg))",
          throttled: "hsl(var(--status-throttled))",
          "throttled-bg": "hsl(var(--status-throttled-bg))",
        },
        node: {
          bg: "hsl(var(--node-bg))",
          border: "hsl(var(--node-border))",
          "border-hover": "hsl(var(--node-border-hover))",
        },
        edge: {
          DEFAULT: "hsl(var(--edge-color))",
        },
      },
      borderRadius: {
        // --radius is 10px: controls sit at sm/md, cards at xl, the app shell
        // at 2xl. Anything rounder than xl on a small element reads as a toy.
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
        xl: "calc(var(--radius) + 4px)",
        "2xl": "calc(var(--radius) + 10px)",
      },
      fontFamily: {
        // "… Variable" is the family name @fontsource-variable registers; the
        // static names stay as a fallback for anyone who installs those instead.
        sans: ["Inter Variable", "Inter", "system-ui", "-apple-system", "sans-serif"],
        mono: ["JetBrains Mono Variable", "JetBrains Mono", "Menlo", "Monaco", "monospace"],
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
      boxShadow: {
        // A card separates with a 1px border, not a shadow — these stay at the
        // edge of perception so they read as a seam, not a drop shadow.
        soft: "0 1px 2px rgba(15, 18, 30, 0.04)",
        card: "0 1px 2px rgba(15, 18, 30, 0.03)",
        // Reserved for layers that genuinely float: popovers, dropdowns, dialogs.
        elevated: "0 12px 32px -12px rgba(15, 18, 30, 0.18), 0 2px 6px -2px rgba(15, 18, 30, 0.06)",
        // Softer replacements for the Tailwind defaults the Radix primitives use.
        md: "0 4px 12px -4px rgba(15, 18, 30, 0.10), 0 1px 3px rgba(15, 18, 30, 0.04)",
        lg: "0 12px 32px -12px rgba(15, 18, 30, 0.18), 0 2px 6px -2px rgba(15, 18, 30, 0.06)",
      },
    },
  },
  plugins: [tailwindcssAnimate],
} satisfies Config;
