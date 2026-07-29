import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, ActionBar, ChoiceRow,
  Toast, KeyValue, Avatar,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { tickets, vendors } from '../src/data/mock';

/**
 * Dispatch — choose a vendor and a slot.
 *
 * Vendors are ranked by how fast they actually respond on this trade, not by
 * rate card, because an emergency that waits four hours costs more than the
 * difference in callout fee.
 */

const slots = [
  'Today, 14:00 – 16:00',
  'Today, 17:00 – 19:00',
  'Tomorrow, 09:00 – 11:00',
  'Tomorrow, 15:00 – 17:00',
];

export default function Dispatch() {
  const router = useRouter();
  const { ticket } = useLocalSearchParams<{ ticket?: string }>();
  const t = tickets.find((x) => x.id === ticket) ?? tickets[0];

  const [vendor, setVendor] = useState(vendors[0].id);
  const [slot, setSlot] = useState(slots[0]);
  const [sent, setSent] = useState(false);

  const chosen = vendors.find((v) => v.id === vendor)!;

  if (sent) {
    return (
      <>
        <BackHeader title="Vendor dispatched" onBack={() => router.back()} />
        <Screen>
          <Toast text="Job order sent — the tenant has been told" />
          <Card>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
              <Avatar initials={chosen.name.split(' ').map((w) => w[0]).join('').slice(0, 2)} size={46} tone="green" />
              <View style={{ flex: 1 }}>
                <Text style={s.h}>{chosen.name}</Text>
                <Text style={s.sub}>{chosen.trade}</Text>
              </View>
            </View>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Job" v={t.title} />
              <KeyValue k="Unit" v={t.unit} />
              <KeyValue k="Slot" v={slot} />
              <KeyValue k="Callout" v={chosen.ratePaise ? inr(chosen.ratePaise) : 'Under AMC'} />
              <KeyValue k="Start code" v="4471 — technician enters this on arrival" last />
            </View>
            <Text style={s.note}>
              The technician sees this in Dwellm8 Pro. Work starts only when the tenant's OTP is
              entered, so an unannounced visit cannot be billed.
            </Text>
          </Card>
          <Button label="Done" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Dispatch a vendor" subtitle={t.title} onBack={() => router.back()} />
      <Screen>
        <Card>
          <Text style={s.h}>Panel vendors for {t.category.toLowerCase()}</Text>
          <Text style={s.sub}>Ranked by median response on this trade in your portfolio.</Text>
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {vendors.map((v, i) => (
            <ListRow
              key={v.id}
              title={v.name}
              subtitle={v.trade}
              meta={`★ ${v.rating} · ${v.jobs} jobs · ~${v.responseMins}m · ${v.ratePaise ? inr(v.ratePaise, { noPaise: true }) + ' callout' : 'under AMC'}`}
              right={
                <View style={{ alignItems: 'flex-end', gap: 6 }}>
                  <StatusPill text={v.onPanel ? 'Panel' : 'Off panel'} tone={v.onPanel ? 'green' : 'neutral'} />
                  <View style={[s.radio, vendor === v.id && { borderColor: color.accent, backgroundColor: color.accent }]} />
                </View>
              }
              onPress={() => setVendor(v.id)}
              last={i === vendors.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Slot</Text>
          {slots.map((sl, i) => (
            <ChoiceRow key={sl} label={sl} selected={slot === sl} onPress={() => setSlot(sl)} last={i === slots.length - 1} />
          ))}
          <Text style={s.note}>
            The tenant confirms the slot before the technician is told. A slot the tenant has not
            accepted stays provisional.
          </Text>
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Send job order" onPress={() => setSent(true)} style={{ flex: 2 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 5, lineHeight: 18 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  radio: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.8, borderColor: color.lineDotted },
});
