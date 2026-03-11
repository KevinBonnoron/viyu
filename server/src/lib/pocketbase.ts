import Pocketbase from 'pocketbase';
import { config } from './env';
import { logger } from './logger';

export const pb = new Pocketbase(config.pocketbase.url);
pb.autoCancellation(false);

async function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function initializePocketBase() {
  const maxRetries = 10;
  const initialDelay = 1000;
  const maxDelay = 10000;

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      await pb.collection('_superusers').authWithPassword(config.pocketbase.superuserEmail, config.pocketbase.superuserPassword);
      logger.info('PocketBase initialized successfully');
      return;
    } catch (error) {
      if (attempt === maxRetries) {
        logger.error('Failed to authenticate with PocketBase after maximum retries');
        throw error;
      }

      // Exponential backoff with max delay
      const delay = Math.min(initialDelay * 2 ** (attempt - 1), maxDelay);
      logger.warn({ attempt, maxRetries, delay }, 'PocketBase not ready, retrying...');
      await sleep(delay);
    }
  }
}
