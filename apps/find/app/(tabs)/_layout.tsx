import { Tabs } from 'expo-router';
import { Text } from 'react-native';
import { SearchIcon, HomeIcon, ClipboardIcon, PlusIcon, color, font, shadow } from '@dwellm8/mobile-shared';

const tab = (Icon: any) => ({ focused }: { focused: boolean }) => (
  <Icon size={26} c={focused ? color.accent : color.inkFaint} w={focused ? 2.1 : 1.8} />
);
const label = (text: string) => ({ focused }: { focused: boolean }) => (
  <Text style={{ ...font.small, color: focused ? color.accent : color.inkSoft, marginTop: 2 }}>{text}</Text>
);

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarStyle: {
          backgroundColor: '#FFFFFF', borderTopColor: color.line,
          height: 84, paddingTop: 8, ...shadow.bar,
        },
      }}
    >
      <Tabs.Screen name="index" options={{ tabBarIcon: tab(SearchIcon), tabBarLabel: label('Search') }} />
      <Tabs.Screen name="saved" options={{ tabBarIcon: tab(HomeIcon), tabBarLabel: label('Saved') }} />
      <Tabs.Screen name="enquiries" options={{ tabBarIcon: tab(ClipboardIcon), tabBarLabel: label('Enquiries') }} />
      <Tabs.Screen name="list" options={{ tabBarIcon: tab(PlusIcon), tabBarLabel: label('List') }} />
    </Tabs>
  );
}
