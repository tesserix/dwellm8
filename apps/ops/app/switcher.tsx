import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import {
  CloseIcon, BuildingIcon, BedIcon, ShieldIcon, StatusPill,
  color, font, radius, shadow, space,
} from '@dwellm8/mobile-shared';
import { org } from '../src/data/mock';

/**
 * Context switch.
 *
 * Roles are scoped to (organisation, property, unit) and never global, so this
 * screen switches the whole app rather than filtering a blended view.
 */

const icons = [<BuildingIcon size={44} c="#9FB0C4" w={1.6} />, <BedIcon size={44} c="#9FB0C4" w={1.6} />, <ShieldIcon size={44} c="#9FB0C4" w={1.6} />];

export default function Switcher() {
  const router = useRouter();
  const [active, setActive] = useState(org.portfolios[0].id);

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.title}>Switch portfolio</Text>
          <Text style={s.sub}>{org.name} · {org.city}</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ padding: space(4), gap: 14 }}>
        {org.portfolios.map((p, i) => {
          const on = p.id === active;
          return (
            <Pressable
              key={p.id}
              style={[s.card, on && s.cardActive]}
              onPress={() => { setActive(p.id); router.back(); }}
            >
              <View style={s.thumb}>{icons[i]}</View>
              <View style={{ flex: 1 }}>
                <Text style={s.name}>{p.name}</Text>
                <Text style={s.meta}>{p.units} units</Text>
              </View>
              {on ? <StatusPill text="Current" tone="green" /> : null}
            </Pressable>
          );
        })}

        <Text style={s.note}>
          Permissions do not carry across a switch. If you also manage a portfolio for another agency,
          it appears under that organisation, signed in separately.
        </Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 3 },
  card: {
    flexDirection: 'row', alignItems: 'center', gap: 14,
    backgroundColor: '#FFF', borderRadius: radius.lg, padding: space(4),
    borderWidth: 2, borderColor: 'transparent', ...shadow.card,
  },
  cardActive: { borderColor: color.accent },
  thumb: { width: 62, height: 62, borderRadius: radius.md, backgroundColor: '#E9EFF8', alignItems: 'center', justifyContent: 'center' },
  name: { ...font.h3, color: color.inkStrong },
  meta: { ...font.small, color: color.inkSoft, marginTop: 3 },
  note: { ...font.small, color: color.inkSoft, lineHeight: 18, marginTop: space(2) },
});
