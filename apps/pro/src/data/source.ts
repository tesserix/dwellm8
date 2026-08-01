/**
 * Dwellm8 Pro's data mode.
 *
 * demo (no EXPO_PUBLIC_API_URL): the §9.6 sample workload renders.
 * live: the vendor module (dwellm8#104, #105) has no API yet, so every screen
 * says so honestly instead of dressing demonstration jobs up as real ones —
 * the rule the Own and Find apps follow for their unserved sections.
 */
import { apiFromEnv } from '@dwellm8/mobile-shared';

export type Mode = 'live' | 'demo';

export function mode(): Mode {
  return apiFromEnv() ? 'live' : 'demo';
}

export const LIVE_EMPTY = {
  title: 'Nothing here yet',
  body:
    'Vendor jobs arrive here once your firm is onboarded to Dwellm8. ' +
    'The demonstration workload shows how the app will feel.',
};
