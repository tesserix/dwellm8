import { useCallback } from 'react';
import { useRouter } from 'expo-router';

/**
 * Back, for a screen that may have nothing behind it (#347).
 *
 * A deep link or a reload leaves the stack empty, and router.back() there is a
 * button that does nothing — so the fallback route is where the screen belongs
 * in the app, not where the user came from.
 */
export function useBack(fallback: string = '/'): () => void {
  const router = useRouter();
  return useCallback(() => {
    if (router.canGoBack()) {
      router.back();
      return;
    }
    router.replace(fallback as never);
  }, [router, fallback]);
}
