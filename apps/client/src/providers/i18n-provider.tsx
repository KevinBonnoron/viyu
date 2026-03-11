import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { en, type TranslationKey } from './translations/en';
import { fr } from './translations/fr';

export type { TranslationKey } from './translations/en';

type Language = 'en' | 'fr';
type Variables = Record<string, string | number>;
type I18nContextValue = {
  language: Language;
  setLanguage: (language: Language) => void;
  t: (key: TranslationKey, variables?: Variables) => string;
};

const I18nContext = createContext<I18nContextValue | undefined>(undefined);
const LANGUAGE_KEY = 'viyu-language';

function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(LANGUAGE_KEY);
    if (saved === 'en' || saved === 'fr') {
      return saved;
    }
  } catch {
    // Language selection remains usable when browser storage is unavailable.
  }
  return typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('fr') ? 'fr' : 'en';
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, updateLanguage] = useState<Language>(initialLanguage);
  const setLanguage = useCallback((next: Language) => {
    if (next === 'en' || next === 'fr') {
      updateLanguage(next);
    }
  }, []);

  useEffect(() => {
    document.documentElement.lang = language;
    try {
      localStorage.setItem(LANGUAGE_KEY, language);
    } catch {
      // Do not block rendering or language changes if storage is disabled.
    }
  }, [language]);

  const t = useCallback(
    (key: TranslationKey, variables?: Variables) => {
      const translated = language === 'fr' ? fr[key] : en[key];
      return translated.replace(/\{([^{}]+)\}/g, (placeholder, name: string) =>
        variables && Object.hasOwn(variables, name) ? String(variables[name]) : placeholder,
      );
    },
    [language],
  );
  const value = useMemo(() => ({ language, setLanguage, t }), [language, setLanguage, t]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) {
    throw new Error('useI18n must be used within I18nProvider');
  }
  return context;
}
