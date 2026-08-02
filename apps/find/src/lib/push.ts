/**
 * Push registration and the notification deep link (#126).
 *
 * Best-effort throughout: a declined permission, a simulator, or demo mode
 * all leave the app fully working — the alert simply arrives nowhere, which
 * is the user's stated preference, not an error.
 */

import { useEffect } from 'react';
import { Platform } from 'react-native';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import { useRouter } from 'expo-router';
import { api, prospectToken } from '../data/source';

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowBanner: true,
    shouldShowList: true,
    shouldPlaySound: false,
    shouldSetBadge: false,
  }),
});

/** Ask once, register the Expo token against the prospect session. */
export async function registerForAlerts(): Promise<boolean> {
  const client = api();
  if (!client || !Device.isDevice) return false;
  try {
    let { status } = await Notifications.getPermissionsAsync();
    if (status !== 'granted') {
      ({ status } = await Notifications.requestPermissionsAsync());
    }
    if (status !== 'granted') return false;
    const expo = await Notifications.getExpoPushTokenAsync();
    const token = await prospectToken(client);
    await client.registerPushToken(token, expo.data,
      Platform.OS === 'ios' ? 'ios' : 'android');
    return true;
  } catch {
    return false; // reachability is optional; the app never is
  }
}

/** Routes a notification tap to the exact screen its data names. */
export function useNotificationDeepLink() {
  const router = useRouter();
  useEffect(() => {
    const sub = Notifications.addNotificationResponseReceivedListener((response) => {
      const url = response.notification.request.content.data?.url;
      if (typeof url === 'string' && url.startsWith('/')) {
        router.push(url as never);
      }
    });
    // A tap that launched the app cold arrives before any listener existed.
    Notifications.getLastNotificationResponseAsync().then((response) => {
      const url = response?.notification.request.content.data?.url;
      if (typeof url === 'string' && url.startsWith('/')) {
        router.push(url as never);
      }
    });
    return () => sub.remove();
  }, [router]);
}
