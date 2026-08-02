import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, ChipRow, DottedRule, EmptyState, HouseArt,
  Screen, WrenchIcon, color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { useLiveData } from '../../src/data/source';

export default function Requests() {
  const router = useRouter();
  const [filter, setFilter] = useState('Open');
  const { tickets } = useLiveData();
  const settled = (s: string) => s === 'Resolved' || s === 'Cancelled';
  const list = tickets.filter((t) => (filter === 'Open' ? !settled(t.status) : settled(t.status)));

  return (
    <>
      <AppHeader title="Requests" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <Pressable style={s.cta} onPress={() => router.push('/raise')}>
          <WrenchIcon size={20} c="#FFF" />
          <Text style={s.ctaText}>Raise a request</Text>
        </Pressable>

        <ChipRow items={[{ label: 'Open' }, { label: 'Resolved' }]} value={filter} onChange={setFilter} />

        {list.length === 0 ? (
          <EmptyState art={<HouseArt size={180} />} title="Nothing open" body="Raise a request and we'll take it from there." />
        ) : null}

        {list.map((t) => (
          <Pressable key={t.id} onPress={() => router.push(`/ticket?id=${t.id}`)}>
            <Card>
              <View style={s.top}>
                <Text style={s.status}>{t.status.toUpperCase()}</Text>
                <Text style={s.cat}>{t.category}</Text>
              </View>
              <Text style={s.title}>{t.title}</Text>
              <Text style={s.meta}>Raised {t.raised}</Text>
              <DottedRule />
              <View style={s.payRow}>
                <Text style={s.payLabel}>
                  {t.liability === 'Tenant' ? 'You pay'
                    : t.liability === 'Owner' ? 'Owner pays'
                    : t.liability === 'Shared' ? 'Shared cost'
                    : 'Being assessed'}
                </Text>
                {t.costPaise ? (
                  <Text style={[s.payValue, t.liability === 'Tenant' && { color: color.negative }]}>
                    {inr(t.costPaise)}
                  </Text>
                ) : null}
              </View>
            </Card>
          </Pressable>
        ))}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  cta: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 9,
    backgroundColor: color.accent, borderRadius: radius.pill,
    marginHorizontal: space(4), marginTop: space(4), paddingVertical: space(4),
  },
  ctaText: { ...font.h3, color: '#FFF' },
  top: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 },
  status: { ...font.tiny, color: color.accentDeep, fontSize: 12 },
  cat: { ...font.small, color: color.inkSoft },
  title: { ...font.h3, color: color.inkStrong },
  meta: { ...font.small, color: color.inkSoft, marginTop: 3, marginBottom: space(3) },
  payRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingTop: space(3) },
  payLabel: { ...font.label, color: color.ink },
  payValue: { ...font.label, color: color.inkStrong },
});
