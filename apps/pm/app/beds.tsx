import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
 Avatar, BackHeader, Button, Card, color, count, EmptyState, ErrorState, Field, font, inr,
  KeyValue, Metric, MetricRow, radius, Screen, space, StatusPill, Toast, useBack,
} from '@dwellm8/mobile-shared';
import type { OpsBed } from '@dwellm8/mobile-shared';
import { useBeds } from '../src/data/beds';
import { usePortfolio } from '../src/data/portfolio';
import { usePropertyRecord } from '../src/data/property';

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
  const goBack = useBack('/(tabs)');
  const params = useLocalSearchParams<{ id?: string }>();
  const portfolio = usePortfolio();
  // Opened from Quick actions there is no property in hand: the first
  // co-living building under management is the one a warden means.
  const property = params.id
    ? portfolio.rows.find((p) => p.id === params.id)
    : portfolio.rows.find((p) => p.kind === 'coliving');
  const board = useBeds(property?.id ?? '');
  const record = usePropertyRecord(property?.id);

  const [picked, setPicked] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [room, setRoom] = useState<string | null>(null);
  const [label, setLabel] = useState('');
  const [rent, setRent] = useState('');
  const [adding, setAdding] = useState(false);
  const bed = board.beds.find((b) => b.id === picked);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const addBed = async () => {
    if (!room) {
      say('Pick which room the bed goes in.');
      return;
    }
    setAdding(true);
    try {
      await board.add(room, label, Math.round(Number(rent || 0) * 100));
      // Both fields empty for the next bed: a rent left in the box is typed
      // over rather than replaced, and lands as a number nobody meant.
      setLabel('');
      setRent('');
      say(`Bed ${label.trim()} is on the register`);
    } catch (err) {
      say((err as Error).message);
    } finally {
      setAdding(false);
    }
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
        <BackHeader title="Bed allocation" onBack={goBack} />
        <Screen><View style={s.waiting}><ActivityIndicator /></View></Screen>
      </>
    );
  }

  if (!property) {
    return (
      <>
        <BackHeader title="Bed allocation" onBack={goBack} />
        <Screen>
          <EmptyState
            full
            title="No hostel or PG yet"
            body="Beds are allocated in a co-living building. Onboard one and its rooms, and the board appears here."
            action="Onboard a hostel"
            onAct={() => router.push('/onboard')}
          />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader
        title={property.name}
        subtitle={`${count(board.beds.length, 'bed')} under management`}
        onBack={goBack}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {board.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {board.error ? <ErrorState error={board.error} onRetry={board.reload} /> : null}

        {!board.loading && !board.error ? (
          <>
            <MetricRow>
              <Metric value={String(board.occupied)} label="beds occupied" tone="green" />
              <Metric value={String(board.vacant)} label="vacant now" tone="amber" />
              <Metric value={String(board.onNotice)} label="on notice" tone="red" />
            </MetricRow>

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
                body="Nobody can be allocated a bed the register does not hold. Put the beds in the rooms below and the board fills itself."
              />
            ) : null}

            <Card>
              <Text style={s.h}>Put a bed in a room</Text>
              <Text style={s.sub}>A bed is what is let in a PG, so it carries its own rent.</Text>
              {record.units.length ? (
                <View style={s.rooms}>
                  {record.units.map((u) => (
                    <Pressable
                      key={u.id}
                      accessibilityRole="button"
                      accessibilityState={{ selected: room === u.id }}
                      onPress={() => setRoom(u.id)}
                      style={[s.chip, room === u.id && s.chipOn]}
                    >
                      <Text style={[s.chipInk, room === u.id && s.chipInkOn]}>{u.code}</Text>
                    </Pressable>
                  ))}
                </View>
              ) : (
                <Text style={s.note}>This building has no rooms on the register yet.</Text>
              )}
              <Field label="What the bed is called" value={label} onChange={setLabel}
                placeholder="Bed — G1-A, upper bunk" autoCapitalize="characters" />
              <Field label="Rent for this bed" value={rent} onChange={setRent}
                keyboardType="numeric" placeholder="Rent per month, ₹" />
              <Button label="Add this bed" onPress={addBed} disabled={adding}
                style={{ marginTop: space(4) }} />
            </Card>

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
  rooms: { flexDirection: 'row', flexWrap: 'wrap', gap: space(2), marginTop: space(3), marginBottom: space(4) },
  chip: {
    minHeight: 40, justifyContent: 'center', paddingHorizontal: space(4),
    borderRadius: radius.pill, borderWidth: 1, borderColor: color.line,
  },
  chipOn: { backgroundColor: color.accent, borderColor: color.accent },
  chipInk: { ...font.small, color: color.inkSoft },
  chipInkOn: { color: '#FFFFFF', fontWeight: '600' },
});
