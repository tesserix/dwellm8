import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Metric, StatusPill, Button, Avatar,
  Toast, KeyValue, EmptyState, ErrorState,
  color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import type { OpsBed } from '@dwellm8/mobile-shared';
import { useBeds } from '../src/data/beds';
import { usePortfolio } from '../src/data/portfolio';

/**
 * Bed allocation for the PG vertical (#299).
 *
 * A hostel warden thinks in a floor plan, not a table, so beds render as a
 * grid coloured by state; tapping one opens the allocation decision.
 */

const tones: Record<OpsBed['state'], { bg: string; ink: string; border: string }> = {
  vacant: { bg: '#F3F6FA', ink: color.inkSoft, border: color.line },
  reserved: { bg: '#EDEAFA', ink: '#5B4CC4', border: '#D6D0F5' },
  notice: { bg: '#FBEEDC', ink: '#B0731C', border: '#EFD9B6' },
  occupied: { bg: '#E8F4E0', ink: '#4E8A2C', border: '#D3E8C6' },
};

const pillTone = { vacant: 'amber', reserved: 'violet', notice: 'amber', occupied: 'green' } as const;

export default function Beds() {
  const router = useRouter();
  const params = useLocalSearchParams<{ id?: string }>();
  const portfolio = usePortfolio();
  // Opened from Quick actions there is no property in hand: the first
  // co-living building under management is the one a warden means.
  const property = params.id
    ? portfolio.rows.find((p) => p.id === params.id)
    : portfolio.rows.find((p) => p.kind === 'coliving');
  const board = useBeds(property?.id ?? '');

  const [picked, setPicked] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const bed = board.beds.find((b) => b.id === picked);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const move = async (id: string, state: OpsBed['state'], label: string) => {
    try {
      await board.allocate(id, state);
      say(`Bed ${label} is now ${state}`);
    } catch (err) {
      say((err as Error).message);
    }
  };

  if (portfolio.loading) {
    return (
      <>
        <BackHeader title="Bed allocation" onBack={() => router.back()} />
        <Screen><View style={s.waiting}><ActivityIndicator /></View></Screen>
      </>
    );
  }

  if (!property) {
    return (
      <>
        <BackHeader title="Bed allocation" onBack={() => router.back()} />
        <Screen>
          <EmptyState
            title="No hostel or PG yet"
            body="Beds are allocated in a co-living building. Onboard one and its rooms, and the board appears here."
          />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader
        title={property.name}
        subtitle={`${board.beds.length} beds under management`}
        onBack={() => router.back()}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {board.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {board.error ? <ErrorState error={board.error} onRetry={board.reload} /> : null}

        {!board.loading && !board.error ? (
          <>
            <View style={s.metrics}>
              <Metric value={String(board.occupied)} label="beds occupied" tone="green" />
              <Metric value={String(board.vacant)} label="vacant now" tone="amber" />
              <Metric value={String(board.onNotice)} label="on notice" tone="red" />
            </View>

            {board.floors.map((f) => (
              <Card key={f.floor}>
                <Text style={s.h}>Floor {f.floor}</Text>
                <View style={s.grid}>
                  {f.beds.map((b) => {
                    const t = tones[b.state];
                    return (
                      <Pressable
                        key={b.id}
                        accessibilityRole="button"
                        accessibilityLabel={`Bed ${b.label}, ${b.state}`}
                        onPress={() => setPicked(b.id === picked ? null : b.id)}
                        style={[
                          s.bed,
                          { backgroundColor: t.bg, borderColor: picked === b.id ? color.accent : t.border },
                        ]}
                      >
                        <Text style={[s.bedLabel, { color: t.ink }]}>{b.label}</Text>
                        <Text style={[s.bedWho, { color: t.ink }]} numberOfLines={1}>{b.state}</Text>
                      </Pressable>
                    );
                  })}
                </View>
                <View style={s.legend}>
                  {(Object.keys(tones) as OpsBed['state'][]).map((k) => (
                    <View key={k} style={s.legendItem}>
                      <View style={[s.legendDot, { backgroundColor: tones[k].bg, borderColor: tones[k].border }]} />
                      <Text style={s.legendText}>{k}</Text>
                    </View>
                  ))}
                </View>
              </Card>
            ))}

            {!board.beds.length ? (
              <EmptyState
                title="No beds on the register"
                body="Add the rooms of this building and the beds in them, and the board fills itself."
              />
            ) : null}

            {bed ? (
              <Card>
                <View style={s.bedHead}>
                  <Avatar initials={bed.label} size={46} tone={bed.state === 'occupied' ? 'green' : 'blue'} />
                  <View style={{ flex: 1 }}>
                    <Text style={s.h}>Bed {bed.label}</Text>
                    <Text style={s.sub}>
                      Room {bed.room} · {bed.sharing}-sharing · floor {bed.floor}
                    </Text>
                  </View>
                  <StatusPill text={bed.state} tone={pillTone[bed.state]} />
                </View>

                <View style={{ marginTop: space(4) }}>
                  <KeyValue k="Rent" v={`${inr(bed.rent_amount_minor, { noPaise: true })} per month`} />
                  <KeyValue k="Tenancy" v={bed.lease_id ? 'Let under a tenancy' : 'None'} last />
                </View>

                <View style={s.actions}>
                  {bed.state === 'vacant' ? (
                    <Button
                      label="Hold this bed"
                      onPress={() => move(bed.id, 'reserved', bed.label)}
                      style={{ flex: 1 }}
                    />
                  ) : (
                    <Button
                      label="Free this bed"
                      tone="secondary"
                      onPress={() => move(bed.id, 'vacant', bed.label)}
                      style={{ flex: 1 }}
                    />
                  )}
                  <Button
                    label="Move-out check"
                    tone="secondary"
                    onPress={() => router.push('/inspection')}
                    style={{ flex: 1 }}
                  />
                </View>
                {bed.state === 'vacant' || bed.state === 'reserved' ? (
                  <Text style={s.note}>
                    Letting a bed means starting the tenancy that pays for it — do that from the
                    property record, and the bed follows.
                  </Text>
                ) : null}
              </Card>
            ) : (
              <Card><Text style={s.sub}>Tap a bed to hold it, free it or start a move-out.</Text></Card>
            )}
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  note: { ...font.small, color: color.inkFaint, marginTop: space(3), lineHeight: 18 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(3) },
  bed: {
    width: 76, height: 62, borderRadius: radius.md, borderWidth: 1.5,
    alignItems: 'center', justifyContent: 'center', paddingHorizontal: 4,
  },
  bedLabel: { ...font.title, fontSize: 15 },
  bedWho: { ...font.small, fontSize: 11, marginTop: 2 },
  bedHead: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  actions: { flexDirection: 'row', gap: 10, marginTop: space(4) },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginTop: space(4) },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 5 },
  legendDot: { width: 12, height: 12, borderRadius: 4, borderWidth: 1.5 },
  legendText: { ...font.small, color: color.inkSoft },
});
