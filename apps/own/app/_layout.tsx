import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import {
  Shell,
  color,
} from '@dwellm8/mobile-shared';

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <StatusBar style="dark" />
      <Shell>
      <Stack
        screenOptions={{
          headerShown: false,
          contentStyle: { backgroundColor: color.bgTop },
        }}
      >
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="switcher" options={{ presentation: 'modal' }} />
        <Stack.Screen name="property" options={{ presentation: 'modal' }} />
        <Stack.Screen name="job" options={{ presentation: 'modal' }} />
        <Stack.Screen name="approve" options={{ presentation: 'modal' }} />
        <Stack.Screen name="payouts" />
        <Stack.Screen name="tax" />
        <Stack.Screen name="documents" />
        <Stack.Screen name="access" />
        <Stack.Screen name="profile" />
        <Stack.Screen name="thread" />
      </Stack>
      </Shell>
    </SafeAreaProvider>
  );
}
