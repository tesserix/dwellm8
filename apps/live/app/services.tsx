import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, Button, StatusPill, KeyValue, Toast,
  ActionBar, ChoiceRow,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { services } from '../src/data/mock';

/**
 * Book a service.
 *
 * Paid-for extras, clearly separated from a maintenance request: this is work
 * the tenant chooses and pays for, and the price is on the screen before
 * anything is booked.
 */

export default function Services() {
  const router = useRouter();
  const [picked, setPicked] = useState<string | null>(null);
  const [slot, setSlot] = useState(0);
  const [booked, setBooked] = useState(false);

  const svc = services.find((x) => x.id === picked);
  const slots = ['Tomorrow, 10:00 – 13:00', 'Saturday, 09:00 – 12:00', 'Sunday, 15:00 – 18:00'];

  if (booked && svc) {
    return (
      <>
        <BackHeader title="Booked" onBack={() => router.back()} />
        <Screen>
          <Toast text="Confirmed — you will get a reminder the day before" />
          <Card>
            <StatusPill text="Confirmed" tone="green" dot />
            <Text style={s.h1}>{svc.name}</Text>
            <KeyValue k="When" v={slots[slot]} />
            <KeyValue k="Who" v={svc.vendor} />
            <KeyValue k="Price" v={inr(svc.pricePaise)} />
            <KeyValue k="Payment" v="After the work, from the app" last />
            <Text style={s.note}>
              This is your booking, not your landlord's — it does not appear on the owner's statement
              and it is not a maintenance request.
            </Text>
          </Card>
          <Button label="Done" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Book a service" subtitle="Vetted vendors, fixed prices" onBack={() => router.back()} />
      <Screen>
        <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
          {services.map((sv, i) => (
            <ListRow
              key={sv.id}
              title={sv.name}
              subtitle={sv.detail}
              meta={`${sv.vendor} · next ${sv.slot}`}
              right={
                <Text style={s.price}>{sv.pricePaise ? inr(sv.pricePaise, { noPaise: true }) : 'On quote'}</Text>
              }
              onPress={() => setPicked(sv.id === picked ? null : sv.id)}
              last={i === services.length - 1}
              tone={picked === sv.id ? 'blue' : undefined}
            />
          ))}
        </Card>

        {svc ? (
          <Card>
            <Text style={s.h}>Pick a slot</Text>
            {slots.map((sl, i) => (
              <ChoiceRow key={sl} label={sl} selected={slot === i} onPress={() => setSlot(i)} last={i === slots.length - 1} />
            ))}
            <View style={{ marginTop: space(3) }}>
              <KeyValue k={svc.name} v={inr(svc.pricePaise)} />
              <KeyValue k="Platform fee" v="None — you pay the vendor's price" tone="green" last />
            </View>
          </Card>
        ) : (
          <Card>
            <Text style={s.body}>
              Choose a service to see slots. Anything that is the landlord's responsibility — a leak,
              a failed geyser, an unsafe fitting — is a maintenance request instead, and costs you
              nothing.
            </Text>
            <Button label="Raise a maintenance request" tone="secondary" onPress={() => router.push('/raise')} style={{ marginTop: space(4) }} />
          </Card>
        )}
      </Screen>

      {svc ? (
        <ActionBar>
          <Button label={`Book for ${inr(svc.pricePaise, { noPaise: true })}`} onPress={() => setBooked(true)} style={{ flex: 1 }} />
        </ActionBar>
      ) : null}
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3), marginBottom: space(2) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, lineHeight: 21 },
  price: { ...font.title, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
