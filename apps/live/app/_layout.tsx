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
          <Stack.Screen name="pay-confirm" options={{ presentation: 'modal' }} />
          <Stack.Screen name="raise" options={{ presentation: 'modal' }} />
          <Stack.Screen name="ticket" options={{ presentation: 'modal' }} />
          <Stack.Screen name="documents" options={{ presentation: 'modal' }} />
          <Stack.Screen name="autopay" options={{ presentation: 'modal' }} />
          <Stack.Screen name="tenancy" options={{ presentation: 'modal' }} />
          <Stack.Screen name="visitors" options={{ presentation: 'modal' }} />
          <Stack.Screen name="services" options={{ presentation: 'modal' }} />
          <Stack.Screen name="profile" />
        </Stack>
      </Shell>
    </SafeAreaProvider>
  );
}
