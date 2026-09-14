/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#0B1220',
        'ink-2': '#111C33',
        navy: '#111C33',
        canvas: '#F4F7F9',
        panel: '#FFFFFF',
        line: '#DCE4EA',
        teal: {
          DEFAULT: '#0F9F73',
          bright: '#2EE6A6',
          dim: '#0B825F',
        },
        slatex: '#64748B',
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'Segoe UI', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      borderRadius: {
        lg: '16px',
        md: '10px',
        sm: '6px',
      },
      boxShadow: {
        glow: '0 0 28px -6px rgba(46, 230, 166, 0.28)',
        'glow-sm': '0 0 14px -3px rgba(46, 230, 166, 0.30)',
        card: '0 1px 2px rgba(15, 23, 42, 0.04), 0 8px 24px rgba(15, 23, 42, 0.05)',
      },
      letterSpacing: {
        wordmark: '0.16em',
      },
    },
  },
  plugins: [],
}
