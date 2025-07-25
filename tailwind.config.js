module.exports = {
  content: [
    "./internal/webui/templates/**/*.templ",
    "./internal/webui/templates/**/*_templ.go"
  ],
  darkMode: 'class',
  safelist: [
    // Z-index classes for modal layering
    'z-60',
    
    // Severity color classes that are applied conditionally
    'bg-severity-critical-light', 'bg-severity-critical-dark',
    'bg-severity-critical-bg-light', 'bg-severity-critical-bg-dark',
    'text-severity-critical-text-light', 'text-severity-critical-text-dark',
    'border-l-severity-critical-light', 'border-l-severity-critical-dark',
    
    'bg-severity-critical-daytime-light', 'bg-severity-critical-daytime-dark',
    'bg-severity-critical-daytime-bg-light', 'bg-severity-critical-daytime-bg-dark',
    'text-severity-critical-daytime-text-light', 'text-severity-critical-daytime-text-dark',
    'border-l-severity-critical-daytime-light', 'border-l-severity-critical-daytime-dark',
    
    'bg-severity-warning-light', 'bg-severity-warning-dark',
    'bg-severity-warning-bg-light', 'bg-severity-warning-bg-dark',
    'text-severity-warning-text-light', 'text-severity-warning-text-dark',
    'border-l-severity-warning-light', 'border-l-severity-warning-dark',
    
    'bg-severity-info-light', 'bg-severity-info-dark',
    'bg-severity-info-bg-light', 'bg-severity-info-bg-dark',
    'text-severity-info-text-light', 'text-severity-info-text-dark',
    'border-l-severity-info-light', 'border-l-severity-info-dark',

    // Background opacity variants
    'bg-severity-critical-bg-light/20', 'bg-severity-critical-bg-dark/20',
    'bg-severity-critical-daytime-bg-light/20', 'bg-severity-critical-daytime-bg-dark/20',
    'bg-severity-warning-bg-light/20', 'bg-severity-warning-bg-dark/20',
    'bg-severity-info-bg-light/20', 'bg-severity-info-bg-dark/20'
  ],
  theme: {
    extend: {
      colors: {
        // Enhanced dark mode colors for better contrast and modern look
        dark: {
          bg: {
            primary: '#0f1114',      // Main background - deeper black
            secondary: '#1a1d21',    // Cards/sections - slightly lighter
            tertiary: '#242830',     // Hover states - visible difference
            elevated: '#2d323b',     // Modals/dropdowns - elevated surface
          },
          border: {
            subtle: '#2d323b',       // Subtle borders
            default: '#3a404d',      // Default borders - more visible
            strong: '#4a5264',       // Strong emphasis borders
          }
        },
        // Modern primary colors with better accessibility
        primary: {
          50: '#eff6ff',
          100: '#dbeafe',
          200: '#bfdbfe',
          300: '#93c5fd',
          400: '#60a5fa',
          500: '#3b82f6',
          600: '#2563eb',
          700: '#1d4ed8',
          800: '#1e40af',
          900: '#1e3a8a',
        },
        // Severity colors optimized for both themes
        severity: {
          critical: {
            light: '#dc2626',        // red-600 for light mode
            dark: '#ef4444',         // red-500 for dark mode
            bg: {
              light: '#fee2e2',      // red-100
              dark: '#7f1d1d',       // red-900
            },
            text: {
              light: '#991b1b',      // red-800
              dark: '#fca5a5',       // red-300
            }
          },
          'critical-daytime': {
            light: '#be123c',        // rose-700 for light mode
            dark: '#fb7185',         // rose-400 for dark mode
            bg: {
              light: '#ffe4e6',      // rose-100
              dark: '#881337',       // rose-900
            },
            text: {
              light: '#9f1239',      // rose-800
              dark: '#fda4af',       // rose-300
            }
          },
          warning: {
            light: '#d97706',        // amber-600 for light mode
            dark: '#fbbf24',         // amber-400 for dark mode - bright yellow
            bg: {
              light: '#fef3c7',      // amber-100
              dark: '#451a03',       // amber-950 - darker background for better contrast
            },
            text: {
              light: '#92400e',      // amber-800
              dark: '#fef3c7',       // amber-100 - much lighter text
            }
          },
          info: {
            light: '#2563eb',        // blue-600 for light mode
            dark: '#60a5fa',         // blue-400 for dark mode
            bg: {
              light: '#dbeafe',      // blue-100
              dark: '#1e3a8a',       // blue-900
            },
            text: {
              light: '#1e40af',      // blue-800
              dark: '#93c5fd',       // blue-300
            }
          },
          success: {
            light: '#059669',        // emerald-600 for light mode
            dark: '#34d399',         // emerald-400 for dark mode
            bg: {
              light: '#d1fae5',      // emerald-100
              dark: '#064e3b',       // emerald-900
            },
            text: {
              light: '#065f46',      // emerald-800
              dark: '#a7f3d0',       // emerald-200
            }
          }
        }
      },
      animation: {
        'fade-in': 'fadeIn 0.2s ease-in-out',
        'slide-in': 'slideIn 0.3s ease-out',
        'pulse-soft': 'pulseSoft 2s ease-in-out infinite',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideIn: {
          '0%': { transform: 'translateY(-10px)', opacity: '0' },
          '100%': { transform: 'translateY(0)', opacity: '1' },
        },
        pulseSoft: {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.8' },
        },
      },
      boxShadow: {
        'dark-lg': '0 10px 15px -3px rgba(0, 0, 0, 0.3), 0 4px 6px -2px rgba(0, 0, 0, 0.2)',
        'dark-xl': '0 20px 25px -5px rgba(0, 0, 0, 0.3), 0 10px 10px -5px rgba(0, 0, 0, 0.2)',
      }
    }
  },
  plugins: []
}