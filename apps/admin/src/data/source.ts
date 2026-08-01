/**
 * Dwellm8 Pro's data mode.
 *
 * demo (no EXPO_PUBLIC_API_URL): the §9.6 sample workload renders.
 * live: the vendor module (dwellm8#115, #30) has no API yet, so every screen
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
    'Support and control-plane data arrives here once the internal tooling ships. ' +
    'The demonstration workload shows how the app will feel.',
};
