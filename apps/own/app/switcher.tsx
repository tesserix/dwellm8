import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { properties } from '../src/data/mock';
import {
  CloseIcon,
  HomeIcon,
  color,
  font,
  radius,
  shadow,
  space,
} from '@rentora/mobile-shared';

export default function Switcher() {
  const router = useRouter();
  const cards = [{ id: 'all', address: 'All Properties' }, ...properties];

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.title}>Filter by…</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={s.grid}>
        {cards.map((c, i) => {
          const active = i === 0;
          return (
            <Pressable key={c.id} style={[s.card, active && s.cardActive]} onPress={() => router.back()}>
              <View style={[s.thumb, active && { backgroundColor: '#FBE9CE' }]}>
                <HomeIcon size={62} c={active ? '#D08A4B' : '#9FB0C4'} w={1.7} />
                <View style={s.badge}><Text style={s.badgeText}>OWNER</Text></View>
              </View>
              <View style={{ padding: space(3) }}>
                <Text style={s.name} numberOfLines={2}>{c.address}</Text>
              </View>
            </Pressable>
          );
        })}
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 14, padding: space(4) },
  card: {
    width: '46%', backgroundColor: '#FFF', borderRadius: radius.lg,
    overflow: 'hidden', borderWidth: 2, borderColor: 'transparent', ...shadow.card,
  },
  cardActive: { borderColor: color.accent },
  thumb: { height: 120, backgroundColor: '#E9EFF8', alignItems: 'center', justifyContent: 'center' },
  badge: {
    position: 'absolute', top: 10, right: 10,
    backgroundColor: '#3E5C96', borderRadius: radius.pill, paddingHorizontal: 12, paddingVertical: 5,
  },
  badgeText: { ...font.tiny, color: '#FFF' },
  name: { ...font.label, color: color.inkStrong },
});
