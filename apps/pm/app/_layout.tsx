import { useEffect } from 'react';
import { View, ActivityIndicator } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { color, Shell } from '@dwellm8/mobile-shared';
import { SessionProvider, useSession } from '../src/auth/session';

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <StatusBar style="dark" />
      <SessionProvider>
        <Guard>
          <Shell>
            <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: color.bgTop } }}>
              <Stack.Screen name="(tabs)" />
              <Stack.Screen name="signin" />
              <Stack.Screen name="arrear" options={{ presentation: 'modal' }} />
              <Stack.Screen name="receipt" options={{ presentation: 'modal' }} />
              <Stack.Screen name="ticket" options={{ presentation: 'modal' }} />
              <Stack.Screen name="dispatch" options={{ presentation: 'modal' }} />
              <Stack.Screen name="inspection" options={{ presentation: 'modal' }} />
              <Stack.Screen name="property" options={{ presentation: 'modal' }} />
              <Stack.Screen name="beds" />
              <Stack.Screen name="gate" />
              <Stack.Screen name="society" />
              <Stack.Screen name="compliance" />
              <Stack.Screen name="lease-tax" options={{ presentation: 'modal' }} />
              <Stack.Screen name="leads" />
              <Stack.Screen name="screening" />
              <Stack.Screen name="pack" />
              <Stack.Screen name="payouts" />
              <Stack.Screen name="thread" />
              <Stack.Screen name="profile" />
              <Stack.Screen name="switcher" options={{ presentation: 'modal' }} />
            </Stack>
          </Shell>
        </Guard>
      </SessionProvider>
    </SafeAreaProvider>
  );
}

/** Guards in the layout, not per screen: a per-screen redirect flashes the
 * arrears of whoever was signed in last. */
function Guard({ children }: { children: React.ReactNode }) {
  const { session, identity, restoring } = useSession();
  const segments = useSegments();
  const router = useRouter();

  useEffect(() => {
    if (restoring || !identity) return;
    const onSignIn = segments[0] === 'signin';
    if (!session && !onSignIn) router.replace('/signin');
    else if (session && onSignIn) router.replace('/(tabs)');
  }, [session, identity, restoring, segments, router]);

  if (restoring) {
    return (
      <View style={{ flex: 1, backgroundColor: color.bgTop, alignItems: 'center', justifyContent: 'center' }}>
        <ActivityIndicator color={color.accent} />
      </View>
    );
  }
  return <>{children}</>;
}
