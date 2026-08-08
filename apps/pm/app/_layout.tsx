import { useEffect } from 'react';
import { View, ActivityIndicator } from 'react-native';
import { Stack, useGlobalSearchParams, useRouter, useSegments } from 'expo-router';
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
              <Stack.Screen name="verify" />
              <Stack.Screen name="firm" />
              <Stack.Screen name="registration" />
              <Stack.Screen name="arrear" options={{ presentation: 'modal' }} />
              <Stack.Screen name="receipt" options={{ presentation: 'modal' }} />
              <Stack.Screen name="ticket" options={{ presentation: 'modal' }} />
              <Stack.Screen name="dispatch" options={{ presentation: 'modal' }} />
              <Stack.Screen name="inspection" options={{ presentation: 'modal' }} />
              <Stack.Screen name="viewings" />
              <Stack.Screen name="viewing-times" />
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
  const { session, identity, restoring, hasFirm, verified, registered } = useSession();
  const segments = useSegments();
  const router = useRouter();
  const { revisit } = useGlobalSearchParams<{ revisit?: string }>();

  useEffect(() => {
    if (restoring || !identity) return;
    const at = segments[0];
    // A registration opened deliberately is not the onboarding gate, so it is
    // not bounced to the tabs — that is the only way back in to file a RERA
    // registration taken up later (#359).
    const gate = at === 'signin' || at === 'verify' || at === 'firm'
      || (at === 'registration' && !revisit);
    if (!session) {
      if (at !== 'signin') router.replace('/signin');
      return;
    }
    // The gates in the order the law and the schema put them: an address that
    // reaches the manager, then the organisation every /v1/ops call is scoped
    // by, then what the firm must hold to manage anybody else's property.
    if (verified === false) {
      if (at !== 'verify') router.replace('/verify');
    } else if (hasFirm === false) {
      if (at !== 'firm') router.replace('/firm');
    } else if (hasFirm === true && registered === false) {
      if (at !== 'registration') router.replace('/registration');
    } else if (hasFirm === true && registered !== false && gate) {
      router.replace('/(tabs)');
    }
  }, [session, identity, restoring, hasFirm, verified, registered, segments, router, revisit]);

  if (restoring) {
    return (
      <View style={{ flex: 1, backgroundColor: color.bgTop, alignItems: 'center', justifyContent: 'center' }}>
        <ActivityIndicator color={color.accent} />
      </View>
    );
  }
  return <>{children}</>;
}
