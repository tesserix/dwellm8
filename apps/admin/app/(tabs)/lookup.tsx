import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, SearchBar, ListRow, StatusPill, Avatar,
  BuildingIcon, UserIcon,
  color, font, inrShort, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { customers } from '../../src/data/mock';

/**
 * Customer lookup for a support call.
 *
 * Read-heavy by design: an administrator on a call needs to see state and
 * history, not to edit a record from a phone.
 */

const stateTone: Record<string, Tone> = { Active: 'green', Suspended: 'red', Onboarding: 'amber' };

export default function Lookup() {
  const router = useRouter();
  const [q, setQ] = useState('');

  const list = customers.filter(
    (c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || c.detail.toLowerCase().includes(q.toLowerCase()),
  );

  return (
    <>
      <AppHeader title="Lookup" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <View style={{ marginTop: space(4) }}>
          <SearchBar value={q} onChange={setQ} placeholder="Organisation, person, phone or reference" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {list.map((c, i) => (
            <ListRow
              key={c.id}
              left={
                c.kind === 'Organisation'
                  ? <View style={s.icon}><BuildingIcon size={20} /></View>
                  : <Avatar initials={c.name.split(' ').map((w) => w[0]).join('')} />
              }
              title={c.name}
              subtitle={c.detail}
              meta={`Customer since ${c.since}${c.gmvPaise ? ` · ${inrShort(c.gmvPaise)} lifetime volume` : ''}`}
              right={<StatusPill text={c.state} tone={stateTone[c.state]} />}
              onPress={() => router.push(`/customer?id=${c.id}`)}
              last={i === list.length - 1}
            />
          ))}
          {!list.length ? <Text style={s.empty}>Nothing matches that.</Text> : null}
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <UserIcon size={20} />
            <Text style={s.h}>What you can see here</Text>
          </View>
          <Text style={s.body}>
            State, plan, volume and recent events. No Aadhaar number is stored anywhere in dwellm8,
            and bank and PAN details are never rendered in the app — the console shows only the last
            four digits, and only to finance.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  icon: { width: 38, height: 38, borderRadius: 19, backgroundColor: '#F3F7FB', alignItems: 'center', justifyContent: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});
