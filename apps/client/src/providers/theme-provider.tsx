import { createContext, useCallback, useContext, useEffect, useState } from 'react';

export type Theme = 'dark' | 'light' | 'system';

export const ACCENTS = ['teal', 'violet', 'fuchsia', 'rose', 'orange', 'amber', 'lime', 'emerald', 'cyan', 'sky'] as const;
export type Accent = (typeof ACCENTS)[number];
export const DEFAULT_ACCENT: Accent = 'teal';

type ThemeProviderProps = {
  children: React.ReactNode;
  defaultTheme?: Theme;
  storageKey?: string;
};

type ThemeProviderState = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  /** The accent of the active profile; the last one applied is remembered so a reload does not flash the default. */
  accent: Accent;
  setAccent: (accent: Accent) => void;
};

const initialState: ThemeProviderState = {
  theme: 'system',
  setTheme: () => null,
  accent: DEFAULT_ACCENT,
  setAccent: () => null,
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

// Browser storage may be unavailable (a locked-down webview, a privacy mode): the choice then holds for the session only.
function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeStored(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // The choice still applies until the page is closed.
  }
}

export function ThemeProvider({ children, defaultTheme = 'system', storageKey = 'vite-ui-theme', ...props }: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(() => (readStored(storageKey) as Theme) || defaultTheme);
  const [accent, setAccentState] = useState<Accent>(() => {
    const stored = readStored(`${storageKey}-accent`);
    return ACCENTS.find((value) => value === stored) ?? DEFAULT_ACCENT;
  });
  const setAccent = useCallback(
    (next: Accent) => {
      writeStored(`${storageKey}-accent`, next);
      setAccentState(next);
    },
    [storageKey],
  );

  useEffect(() => {
    document.documentElement.dataset.accent = accent;
  }, [accent]);

  useEffect(() => {
    const root = window.document.documentElement;

    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const applyTheme = () => {
      root.classList.remove('light', 'dark');
      root.classList.add(theme === 'system' ? (media.matches ? 'dark' : 'light') : theme);
    };

    applyTheme();
    if (theme === 'system') {
      media.addEventListener('change', applyTheme);
      return () => media.removeEventListener('change', applyTheme);
    }
  }, [theme]);

  const value = {
    theme,
    setTheme: (theme: Theme) => {
      writeStored(storageKey, theme);
      setTheme(theme);
    },
    accent,
    setAccent,
  };

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  );
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext);
  if (context === undefined) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }

  return context;
};
