import json
import urllib.request
import urllib.parse
import time
import os

# mapping wikidata lang -> openfauna filename
lang_files = {
    "af": "data/locales/af.json",
    "ar": "data/locales/ar.json",
    "he": "data/locales/he.json",
    "hi": "data/locales/hi_in.json",
    "id": "data/locales/id.json",
    "ko": "data/locales/ko.json",
    "ml": "data/locales/ml.json",
    "th": "data/locales/th.json",
    "vi": "data/locales/vi_vn.json",
}

def load_locales():
    locales = {}
    for lang, filepath in lang_files.items():
        if os.path.exists(filepath):
            with open(filepath, "r") as f:
                locales[lang] = json.load(f)
        else:
            locales[lang] = {}
    return locales

def save_locales(locales):
    for lang, filepath in lang_files.items():
        with open(filepath, "w", encoding="utf-8") as f:
            json.dump(locales[lang], f, indent=2, ensure_ascii=False, sort_keys=True)
            f.write("\n")

def chunker(seq, size):
    return (seq[pos:pos + size] for pos in range(0, len(seq), size))

def query_wikidata(scientific_names):
    values = " ".join([f'"{name}"' for name in scientific_names])
    langs_str = ", ".join([f'"{lang}"' for lang in lang_files.keys()])
    query = f"""
    SELECT ?scientificName ?label (LANG(?label) AS ?lang) WHERE {{
      VALUES ?scientificName {{ {values} }}
      ?item wdt:P225 ?scientificName .
      ?item rdfs:label ?label .
      FILTER(LANG(?label) IN ({langs_str}))
    }}
    """
    url = "https://query.wikidata.org/sparql"
    req = urllib.request.Request(url, data=urllib.parse.urlencode({"query": query}).encode('utf-8'))
    req.add_header("Accept", "application/json")
    req.add_header("User-Agent", "OpenFaunaBot/1.0 (tphakala@koti)")
    
    for _ in range(3):
        try:
            with urllib.request.urlopen(req) as response:
                return json.loads(response.read().decode('utf-8'))
        except Exception as e:
            print(f"Error querying Wikidata: {e}")
            time.sleep(5)
    return None

def main():
    os.chdir("/home/thakala/src/openfauna")
    with open("data/metadata.json", "r") as f:
        metadata = json.load(f)
    
    all_names = list(metadata.keys())
    locales = load_locales()
    
    added = {lang: 0 for lang in lang_files.keys()}
    
    batch_size = 300
    print(f"Processing {len(all_names)} species in batches of {batch_size}...")
    
    for i, batch in enumerate(chunker(all_names, batch_size)):
        print(f"Batch {i+1} / {len(all_names)//batch_size + 1}")
        data = query_wikidata(batch)
        if not data:
            continue
            
        for binding in data["results"]["bindings"]:
            sciname = binding["scientificName"]["value"]
            label = binding["label"]["value"]
            lang = binding["lang"]["value"]
            
            if lang in locales and label != sciname:
                if sciname not in locales[lang]:
                    locales[lang][sciname] = label
                    added[lang] += 1
                    
        if i % 5 == 0:
            save_locales(locales)
            
        time.sleep(1)
        
    save_locales(locales)
    print("Done!")
    for lang, count in added.items():
        print(f"Added {count} to {lang}")

if __name__ == "__main__":
    main()
