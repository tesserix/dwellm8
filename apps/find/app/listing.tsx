import React, { useState } from 'react';
import { View, Text, StyleSheet, Image } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, ActionBar, ListRow,
  Toast, CheckCircleIcon, ShieldIcon, AlertIcon, MapPinIcon, UsersIcon,
  color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { listings, photos } from '../src/data/mock';
import { api, prospectToken, useListing } from '../src/data/source';

/**
 * A listing.
 *
 * The verification panel is the reason this app exists: a seeker in India has
 * been burnt by listings that were let months ago, priced wrong, or posted by
 * someone with no right to let. Every check is shown, including the ones that
 * failed.
 */

export default function ListingScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const live = useListing(id);
  const l = live.listing ?? listings.find((x) => x.id === id) ?? listings[0];
  const [saved, setSaved] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  // Saving works without an account: the browsing token is minted on first
  // use, and the shortlist follows it (ADR-0019).
  const save = async () => {
    const client = api();
    if (client && l.id) {
      try {
        const token = await prospectToken(client);
        if (saved) await client.shortlistRemove(token, l.id);
        else await client.shortlistAdd(token, l.id);
      } catch {
        say('Could not update your saved homes');
        return;
      }
    }
    setSaved(!saved);
    say(saved ? 'Removed from saved' : 'Saved');
  };

  const v = l.verification;
  const checks = [
    { ok: v.ownership, label: 'Ownership document', hint: l.listedBy === 'Owner' ? 'Khata and tax receipt in the owner’s name' : 'Management agreement with the owner' },
    { ok: v.address, label: 'Address', hint: 'Geocoded and matched to the document' },
    { ok: v.identity, label: 'Lister’s identity', hint: v.identity ? 'ID verified and matched to the ownership document' : 'Submitted, not yet matched — treat with care' },
    { ok: v.photosOwn, label: 'Photographs', hint: v.photosOwn ? 'Taken on site by the lister' : 'Not confirmed as taken on site' },
  ];

  return (
    <>
      <BackHeader
        title={l.title}
        subtitle={`${l.locality}, ${l.city}`}
        onBack={() => router.back()}
        right={<Button label={saved ? 'Saved' : 'Save'} tone="secondary" small onPress={save} />}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.hero}>
          <Image source={photos[l.photo]} style={s.heroImg} resizeMode="cover" />
          <View style={s.heroBadges}>
            {l.boosted ? <StatusPill text="Promoted" tone="violet" /> : null}
            {l.managed ? <StatusPill text="Managed by Dwellm8" tone="green" /> : null}
          </View>
        </View>

        <Card>
          <Text style={s.rent}>{inr(l.rentPaise, { noPaise: true })}<Text style={s.per}> per month</Text></Text>
          <Text style={s.dep}>Deposit {inr(l.depositPaise, { noPaise: true })} · {Math.round(l.depositPaise / l.rentPaise)} months</Text>
          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Configuration" v={`${l.bhk} BHK · ${l.baths} bath · ${l.sqft} sq ft`} />
            <KeyValue k="Floor" v={l.floor} />
            <KeyValue k="Furnishing" v={l.furnishing} />
            <KeyValue k="Facing" v={l.facing} />
            <KeyValue k="Available from" v={l.availableFrom} />
            <KeyValue k="Preferred tenants" v={l.preferred} last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>About this home</Text>
          <Text style={s.body}>{l.about}</Text>
          <View style={s.chips}>
            {l.amenities.map((a) => (
              <View key={a} style={s.chip}><Text style={s.chipText}>{a}</Text></View>
            ))}
          </View>
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <ShieldIcon size={20} />
            <Text style={s.h}>What we checked</Text>
          </View>
          <View style={{ marginTop: space(2) }}>
            {checks.map((c, i) => (
              <ListRow
                key={c.label}
                left={c.ok ? <CheckCircleIcon size={20} c={color.positive} /> : <AlertIcon size={20} c="#B0731C" />}
                title={c.label}
                subtitle={c.hint}
                right={<StatusPill text={c.ok ? 'Verified' : 'Pending'} tone={c.ok ? 'green' : 'amber'} />}
                last={i === checks.length - 1}
              />
            ))}
          </View>
          <Text style={s.note}>
            Promotion buys position in search. It never buys a verification badge, and we do not
            rank an unverified listing above a verified one at any price.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>Listed by</Text>
          <KeyValue k={l.listedBy} v={l.lister} />
          <KeyValue k="Posted" v={`${l.postedOn} · ${l.daysLeft} of 90 days left`} />
          <KeyValue k="Replies" v="Usually within 4 hours" />
          <KeyValue k="Seen by" v={`${l.views.toLocaleString('en-IN')} people · ${l.enquiries} enquiries`} last />
        </Card>

        {l.inspections.length ? (
          <Card>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
              <UsersIcon size={20} />
              <Text style={s.h}>Open inspections</Text>
            </View>
            {l.inspections.map((i, idx) => (
              <ListRow
                key={i.id}
                title={i.when}
                subtitle={
                  i.attended != null
                    ? `${i.registered} registered · ${i.attended} actually attended`
                    : `${i.registered} registered so far`
                }
                right={<Button label="Book" small onPress={() => router.push(`/inspect?id=${l.id}&slot=${i.id}`)} />}
                last={idx === l.inspections.length - 1}
              />
            ))}
            <Text style={s.note}>
              You check in at the door with a QR code. It saves the lister guessing who turned up,
              and it means you are not asked for your number three times.
            </Text>
          </Card>
        ) : (
          <Card>
            <Text style={s.h}>No open inspection yet</Text>
            <Text style={s.body}>Ask the lister for a time and we will hold the slot for you.</Text>
          </Card>
        )}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <MapPinIcon size={20} />
            <Text style={s.h}>{l.locality}</Text>
          </View>
          <View style={{ marginTop: space(2) }}>
            <KeyValue k="Metro" v="Whitefield, 2.4 km" />
            <KeyValue k="Schools within 3 km" v="6" />
            <KeyValue k="Median 3 BHK rent here" v="₹41,000" />
            <KeyValue k="This listing" v="2% below the median" tone="green" last />
          </View>
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Ask a question" tone="secondary" onPress={() => say('Question sent to the lister')} style={{ flex: 1 }} />
        <Button label="Apply" onPress={() => router.push(`/apply?id=${l.id}`)} style={{ flex: 1 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  hero: { height: 240, marginHorizontal: space(4), borderRadius: radius.lg, overflow: 'hidden', marginTop: space(3), marginBottom: space(3) },
  heroImg: { width: '100%', height: '100%' },
  heroBadges: { position: 'absolute', top: 12, left: 12, flexDirection: 'row', gap: 6 },
  rent: { ...font.h1, color: color.inkStrong },
  per: { ...font.body, color: color.inkSoft },
  dep: { ...font.body, color: color.inkSoft, marginTop: 4 },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.ink, marginTop: space(2), lineHeight: 22 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(4) },
  chip: { backgroundColor: color.cardMuted, borderRadius: radius.pill, paddingHorizontal: 12, paddingVertical: 6 },
  chipText: { ...font.small, color: color.ink },
});
