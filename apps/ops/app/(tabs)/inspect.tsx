import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Segmented, ListRow, StatusPill, Button,
  Metric, CalendarIcon, MapPinIcon, RouteIcon, ClipboardIcon,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { inspections } from '../../src/data/mock';

/**
 * Inspection calendar and route.
 *
 * Field agents work a day, not a list: today's visits are ordered by their
 * window and carry the drive between them, so the day reads as a route.
 */

const kindTone: Record<string, Tone> = {
  Routine: 'blue',
  'Move-in': 'green',
  'Move-out': 'amber',
  Handover: 'violet',
};

export default function Inspect() {
  const router = useRouter();
  const [when, setWhen] = useState('Today');

  const list = inspections.filter((i) =>
    when === 'Today' ? i.at === 'Today'
    : when === 'Upcoming' ? i.at !== 'Today' && i.status !== 'Submitted'
    : i.status === 'Submitted',
  );

  const totalKm = inspections.filter((i) => i.at === 'Today').reduce((a, i) => a + i.distanceKm, 0);

  return (
    <>
      <AppHeader
        title="Inspections"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={{ marginTop: space(3) }}>
          <Segmented items={['Today', 'Upcoming', 'Submitted']} value={when} onChange={setWhen} />
        </View>

        {when === 'Today' ? (
          <View style={s.metrics}>
            <Metric value={String(list.length)} label="visits today" tone="violet" />
            <Metric value={`${totalKm.toFixed(1)} km`} label="planned route" tone="blue" />
            <Metric value="1" label="report to submit" tone="amber" />
          </View>
        ) : null}

        {list.map((i, idx) => (
          <Card key={i.id}>
            <View style={s.top}>
              <StatusPill text={i.kind} tone={kindTone[i.kind]} />
              <Text style={s.window}>{i.window}</Text>
            </View>
            <Text style={s.unit}>{i.unit}</Text>
            <View style={s.metaRow}>
              <MapPinIcon size={16} c={color.inkSoft} />
              <Text style={s.meta}>{i.locality} · {i.distanceKm} km</Text>
            </View>
            <View style={s.metaRow}>
              <CalendarIcon size={16} c={color.inkSoft} />
              <Text style={s.meta}>Notice: {i.noticeServed}</Text>
            </View>
            <View style={s.metaRow}>
              <ClipboardIcon size={16} c={color.inkSoft} />
              <Text style={s.meta}>{i.tenant}</Text>
            </View>

            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
              <Button
                label={i.status === 'Submitted' ? 'View report' : 'Start inspection'}
                onPress={() => router.push(`/inspection?id=${i.id}`)}
                style={{ flex: 1 }}
                small
              />
              {i.status !== 'Submitted' ? (
                <Button label="Navigate" tone="secondary" onPress={() => {}} small style={{ flex: 1 }} />
              ) : null}
            </View>
            {idx === 0 && when === 'Today' ? (
              <Text style={s.next}>Next stop — leave by 12:05 to arrive in the window.</Text>
            ) : null}
          </Card>
        ))}

        {!list.length ? (
          <Card>
            <Text style={s.empty}>Nothing scheduled in this view.</Text>
          </Card>
        ) : null}

        {when === 'Today' ? (
          <Card>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
              <RouteIcon size={20} />
              <Text style={s.helpTitle}>Route plan</Text>
            </View>
            <Text style={s.helpBody}>
              Baner at 12:30, Marathahalli at 15:00, Whitefield at 17:30. Reordering by distance would
              save 11 km but breaks the notice window at Baner, so the day stays as booked.
            </Text>
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(1) },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  window: { ...font.label, color: color.inkStrong },
  unit: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 7, marginTop: 6 },
  meta: { ...font.small, color: color.inkSoft, flex: 1 },
  next: { ...font.small, color: '#B0731C', marginTop: space(3), fontWeight: '600' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
  helpTitle: { ...font.h3, color: color.inkStrong },
  helpBody: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
});
