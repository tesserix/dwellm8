import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Metric, ListRow, StatusPill, Button, Avatar,
  Toast, KeyValue, Segmented,
  color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { bedWaitlist, beds } from '../src/data/mock';

/**
 * Bed allocation for the PG vertical.
 *
 * A hostel warden thinks in a floor plan, not a table, so beds render as a
 * grid coloured by state; tapping one opens the allocation decision.
 */

const bedTone = (b: (typeof beds)[number]) => {
  if (b.status === 'Vacant') return { bg: '#F3F6FA', ink: color.inkSoft, border: color.line };
  if (b.status === 'Reserved') return { bg: '#EDEAFA', ink: '#5B4CC4', border: '#D6D0F5' };
  if (b.status === 'Notice') return { bg: '#FBEEDC', ink: '#B0731C', border: '#EFD9B6' };
  if (b.dueState === 'Late') return { bg: '#FBE6E4', ink: '#C0433D', border: '#F0CFCB' };
  if (b.dueState === 'Due') return { bg: '#FDF3E2', ink: '#B0731C', border: '#EFDFC0' };
  return { bg: '#E8F4E0', ink: '#4E8A2C', border: '#D3E8C6' };
};

export default function Beds() {
  const router = useRouter();
  const [tab, setTab] = useState('Floor plan');
  const [picked, setPicked] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const bed = beds.find((b) => b.id === picked);
  const occupied = beds.filter((b) => b.status === 'Occupied').length;
  const vacant = beds.filter((b) => b.status === 'Vacant').length;
  const late = beds.filter((b) => b.dueState === 'Late').length;

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const floors = [1, 2];

  return (
    <>
      <BackHeader title="Nest PG, Marathahalli" subtitle="48 beds under management" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(occupied)} label="beds occupied" tone="green" />
          <Metric value={String(vacant)} label="vacant now" tone="amber" />
          <Metric value={String(late)} label="rent late" tone="red" />
        </View>

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Floor plan', 'Waitlist']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Floor plan' ? (
          <>
            {floors.map((f) => (
              <Card key={f}>
                <Text style={s.h}>Floor {f}</Text>
                <View style={s.grid}>
                  {beds.filter((b) => b.floor === f).map((b) => {
                    const t = bedTone(b);
                    return (
                      <Pressable
                        key={b.id}
                        onPress={() => setPicked(b.id === picked ? null : b.id)}
                        style={[
                          s.bed,
                          { backgroundColor: t.bg, borderColor: picked === b.id ? color.accent : t.border },
                        ]}
                      >
                        <Text style={[s.bedLabel, { color: t.ink }]}>{b.label}</Text>
                        <Text style={[s.bedWho, { color: t.ink }]} numberOfLines={1}>
                          {b.resident ? b.resident.split(' ')[0] : b.status}
                        </Text>
                      </Pressable>
                    );
                  })}
                </View>
                <View style={s.legend}>
                  {[
                    ['Paid', '#E8F4E0', '#4E8A2C'],
                    ['Due', '#FDF3E2', '#B0731C'],
                    ['Late', '#FBE6E4', '#C0433D'],
                    ['Vacant', '#F3F6FA', color.inkSoft],
                    ['Reserved', '#EDEAFA', '#5B4CC4'],
                  ].map(([l, bg, ink]) => (
                    <View key={l} style={s.legendItem}>
                      <View style={[s.legendDot, { backgroundColor: bg, borderColor: ink as string }]} />
                      <Text style={s.legendText}>{l}</Text>
                    </View>
                  ))}
                </View>
              </Card>
            ))}

            {bed ? (
              <Card>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
                  <Avatar
                    initials={bed.resident ? bed.resident.split(' ').map((w) => w[0]).join('') : bed.label}
                    size={46}
                    tone={bed.status === 'Occupied' ? 'green' : 'blue'}
                  />
                  <View style={{ flex: 1 }}>
                    <Text style={s.h}>Bed {bed.label}</Text>
                    <Text style={s.sub}>Room {bed.room} · {bed.sharing}-sharing · floor {bed.floor}</Text>
                  </View>
                  <StatusPill text={bed.status} tone={bed.status === 'Occupied' ? 'green' : bed.status === 'Vacant' ? 'amber' : 'violet'} />
                </View>

                <View style={{ marginTop: space(4) }}>
                  <KeyValue k="Resident" v={bed.resident ?? 'None'} />
                  <KeyValue k="Rent" v={`${inr(bed.rentPaise, { noPaise: true })} per month`} />
                  <KeyValue k="Rent state" v={bed.dueState ?? '—'} tone={bed.dueState === 'Late' ? 'red' : bed.dueState === 'Paid' ? 'green' : undefined} last />
                </View>

                <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
                  {bed.status === 'Vacant' || bed.status === 'Reserved' ? (
                    <Button label="Allocate" onPress={() => say(`Bed ${bed.label} allocated from the waitlist`)} style={{ flex: 1 }} />
                  ) : (
                    <Button label="Start move-out" tone="secondary" onPress={() => router.push('/inspection?id=i-5504')} style={{ flex: 1 }} />
                  )}
                  <Button label="Swap bed" tone="secondary" onPress={() => say('Swap requested')} style={{ flex: 1 }} />
                </View>
              </Card>
            ) : (
              <Card>
                <Text style={s.sub}>Tap a bed to allocate, swap or start a move-out.</Text>
              </Card>
            )}
          </>
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {bedWaitlist.map((w, i) => (
              <ListRow
                key={w.id}
                left={<Avatar initials={w.name.split(' ').map((x) => x[0]).join('')} tone="violet" />}
                title={w.name}
                subtitle={w.preference}
                meta={`Wants a bed from ${w.from} · ${w.phone}`}
                right={<Button label="Allocate" small onPress={() => say(`${w.name} allocated to 15B`)} />}
                last={i === bedWaitlist.length - 1}
              />
            ))}
          </Card>
        )}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(3) },
  bed: {
    width: 76, height: 62, borderRadius: radius.md, borderWidth: 1.5,
    alignItems: 'center', justifyContent: 'center', paddingHorizontal: 4,
  },
  bedLabel: { ...font.title, fontSize: 15 },
  bedWho: { ...font.small, fontSize: 11, marginTop: 2 },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginTop: space(4) },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 5 },
  legendDot: { width: 12, height: 12, borderRadius: 4, borderWidth: 1.5 },
  legendText: { ...font.small, color: color.inkSoft },
});
