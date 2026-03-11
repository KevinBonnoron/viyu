function getEnvOrDefault<T>(name: string, defaultValue: T): T {
  return (process.env[name] as T) || defaultValue;
}

export const config = {
  nodeEnv: getEnvOrDefault('ENV', 'development'),
  pocketbase: {
    url: getEnvOrDefault('PB_URL', 'http://localhost:8090'),
    superuserEmail: getEnvOrDefault('PB_SUPERUSER_EMAIL', 'admin@chef-assistant.local'),
    superuserPassword: getEnvOrDefault('PB_SUPERUSER_PASSWORD', 'changeme123'),
  },
};
