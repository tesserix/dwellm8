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
          <Stack.Screen name="listing" />
          <Stack.Screen name="inspect" options={{ presentation: 'modal' }} />
          <Stack.Screen name="apply" options={{ presentation: 'modal' }} />
          <Stack.Screen name="publish" options={{ presentation: 'modal' }} />
          <Stack.Screen name="manage" />
          <Stack.Screen name="profile" />
        </Stack>
      </Shell>
    </SafeAreaProvider>
  );
}
