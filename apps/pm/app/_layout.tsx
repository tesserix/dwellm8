import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { color, Shell } from '@dwellm8/mobile-shared';

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <StatusBar style="dark" />
      <Shell>
        <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: color.bgTop } }}>
          <Stack.Screen name="(tabs)" />
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
    </SafeAreaProvider>
  );
}
