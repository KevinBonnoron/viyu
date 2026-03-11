import { config } from './env';

const level = process.env.LOG_LEVEL ?? (config.nodeEnv === 'production' ? 'error' : 'debug');

const levels = { trace: 0, debug: 1, info: 2, warn: 3, error: 4 } as const;
type Level = keyof typeof levels;
const minLevel = levels[(level as Level) ?? 'info'] ?? levels.info;

// ANSI color codes
const colors = {
  reset: '\x1b[0m',
  dim: '\x1b[2m',
  cyan: '\x1b[36m',
  yellow: '\x1b[33m',
  red: '\x1b[31m',
  magenta: '\x1b[35m',
} as const;

const levelColors: Record<Level, string> = {
  trace: colors.dim,
  debug: colors.magenta,
  info: colors.cyan,
  warn: colors.yellow,
  error: colors.red,
};

function format(method: Level, obj: unknown, msg?: string): string {
  const color = levelColors[method];
  const reset = colors.reset;

  let text: string;
  if (msg !== undefined) {
    text = typeof obj === 'object' && obj !== null ? `${msg} ${JSON.stringify(obj)}` : `${String(obj)} ${msg}`;
  } else {
    text = typeof obj === 'string' ? obj : JSON.stringify(obj);
  }

  return `${color}${text}${reset}`;
}

function log(method: Level) {
  return (obj: unknown, msg?: string) => {
    if (levels[method] >= minLevel) {
      console[method](format(method, obj, msg));
    }
  };
}

const loggerImpl = {
  trace: log('trace'),
  debug: log('debug'),
  info: log('info'),
  warn: log('warn'),
  error: log('error'),
  child: () => loggerImpl,
};

export const logger = loggerImpl;
