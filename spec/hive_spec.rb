require 'airborne'

Airborne.configure do |config|
  config.base_url = 'http://localhost.nytimes.com:8080'
#  config.headers = {'x-auth-token' => 'my_token'}
end

users = nil
asset_id = nil
assignment_id = nil

describe 'hive test' do
  before(:all) do
    print "clearing test database..."
    `curl -s -XDELETE localhost:9200/hivetest`
    puts " done"
  end

  context 'admin' do
    describe 'a project' do
      it 'creates a project' do 
        post '/admin/projects/moshpit', {:Id => 'moshpit', :Name => 'Mosh Pit', :Description => 'also known as slamdancing' }
        expect_json( { :Project => { :Id => 'moshpit', :Name => 'Mosh Pit', :Description => 'also known as slamdancing' } })
      end

      it 'finds a project' do 
        get '/admin/projects/moshpit'
        expect_json({
          :Project => {
            :Id => 'moshpit', :Name => 'Mosh Pit', :Description => 'also known as slamdancing',
            :AssetCount=> regex(/\d/), :TaskCount=>regex(/\d/), :UserCount=>regex(/\d/), :AssignmentCount => {:Total=>regex(/\d/)}
          }
        })
      end

      it 'finds all projects' do
        get '/admin/projects'

        expect_json_types({ :Projects => :array_of_objects })
        expect_json('Projects.?', { 
            :Id => 'moshpit', :Name => 'Mosh Pit', :Description => 'also known as slamdancing',
            :AssetCount=> regex(/\d/), :TaskCount=>regex(/\d/), :UserCount=>regex(/\d/), :AssignmentCount => {:Total=>regex(/\d/)}
        })
      end

      it 'creates a new task' do 
        post '/admin/projects/moshpit/tasks/oi', {:Project => 'moshpit', :Name => 'oi', :Description => 'Does this sound like a british punk rocker?', :CurrentState => "waiting", :AssignmentCriteria => { }, :CompletionCriteria => { :Total => 100, :Matching => 75 } }
        expect_json({ 
          :Task => {
            :Id => "moshpit-oi", :Project => "moshpit", :Name => "oi", :Description => "Does this sound like a british punk rocker?",
            :CurrentState => "waiting",
            :AssignmentCriteria => {:SubmittedData => {}},
            :CompletionCriteria => {:Total => 100, :Matching => 75}
          }
        })
      end

      it 'updates an existing task' do 
        post '/admin/projects/moshpit/tasks/oi', {:Project => 'moshpit', :Name => 'oi', :Description => 'Does this store have Mojo Nixon?', :CurrentState => "available", :AssignmentCriteria => { }, :CompletionCriteria => { :Total => 100, :Matching => 75 } }
        expect_json({ 
          :Task => {
            :Id => "moshpit-oi", :Project => "moshpit", :Name => "oi", :Description => "Does this store have Mojo Nixon?",
            :CurrentState => "available",
            :AssignmentCriteria => {:SubmittedData => {}},
            :CompletionCriteria => {:Total => 100, :Matching => 75}
          }
        })
      end

      it 'enables & disables a task' do 
        post '/admin/projects/moshpit/tasks/linoleum', {:Project => 'moshpit', :Name => 'linoleum', :Description => 'Is this a photo of NOFX?', :CurrentState => "waiting", :AssignmentCriteria => { }, :CompletionCriteria => { :Total => 100, :Matching => 75 } }
        expect_status 200

        get '/admin/projects/moshpit/tasks/linoleum/enable'
        expect_json({ 
          :Task => {
            :Id => "moshpit-linoleum", :Project => "moshpit",
            :CurrentState => "available"
          }
        })

        get '/admin/projects/moshpit/tasks/linoleum/disable'
        expect_json({ 
          :Task => {
            :Id => "moshpit-linoleum", :Project => "moshpit",
            :CurrentState => "waiting"
          }
        })

      end

      it 'finds a task in a project' do 
        get '/admin/projects/moshpit/tasks/oi'
        expect_json({ 
          :Task => {
            :Id => "moshpit-oi", :Project => "moshpit", :Name => "oi", :Description => "Does this store have Mojo Nixon?",
            :CurrentState => "available",
            :AssignmentCriteria => {:SubmittedData => {}},
            :CompletionCriteria => {:Total => 100, :Matching => 75}
          }
        })
      end

      it 'creates multiple tasks' do 
        post '/admin/projects/moshpit/tasks', {:Tasks => [
          { :Project => 'moshpit', :Name => 'oi', :Description => 'Does this sound like a british punk rocker?', :CurrentState => "available", :AssignmentCriteria => { }, :CompletionCriteria => { :Total => 100, :Matching => 75 } },
          { :Project => 'moshpit', :Name => 'tvparty', :Description => 'Is this asset having a TV party tonight?', :CurrentState => "available", :AssignmentCriteria => { }, :CompletionCriteria => { :Total => 5, :Matching => 5 } }
        ]}

        expect_json_types({ :Tasks => :array_of_objects })
        expect_json('Tasks.?', { 
            :Id => "moshpit-oi", :Project => "moshpit", :Name => "oi", :Description => "Does this sound like a british punk rocker?",
            :CurrentState => "available",
            :AssignmentCriteria => {:SubmittedData => {}},
            :CompletionCriteria => {:Total => 100, :Matching => 75}
        })
        expect_json('Tasks.?', { 
          :Id => "moshpit-tvparty", :Project => "moshpit", :Name => "tvparty", :Description => "Is this asset having a TV party tonight?",
          :CurrentState => "available",
          :AssignmentCriteria => {:SubmittedData => {}},
          :CompletionCriteria => {:Total => 5, :Matching => 5}
        })
      end

      it 'finds all tasks for a project' do 
        get '/admin/projects/moshpit/tasks'

        expect_json_types({ :Tasks => :array_of_objects })
        expect_json('Tasks.?', { 
          :Id => "moshpit-oi", :Project => "moshpit", :Name => "oi", :Description => "Does this sound like a british punk rocker?",
          :CurrentState => "available",
          :AssignmentCriteria => {:SubmittedData => {}},
          :CompletionCriteria => {:Total => 100, :Matching => 75}
        })
        expect_json('Tasks.?', { 
          :Id => "moshpit-tvparty", :Project => "moshpit", :Name => "tvparty", :Description => "Is this asset having a TV party tonight?",
          :CurrentState => "available",
          :AssignmentCriteria => {:SubmittedData => {}},
          :CompletionCriteria => {:Total => 5, :Matching => 5}
        })
      end

      it 'returns an empty array when there are no users' do 
        get '/admin/projects/moshpit/users'
        expect_json_types({Users: :array_of_objects, Meta: :object})
      end

      it 'creates a user' do 
        post '/projects/moshpit/user', {:Name => 'Milo Aukerman', :Email => 'milogoestocollege@example.com' }
        expect_json({ :Name => 'Milo Aukerman', :Email => 'milogoestocollege@example.com' })
      end

      it 'finds all users in a project' do 
        get '/admin/projects/moshpit/users'
        users = json_body
        expect_json_types({Users: :array_of_objects, Meta: :object})
        expect_json("Users.?", { :Name => 'Milo Aukerman', :Email => 'milogoestocollege@example.com' })
      end

      it 'finds a single user in a project' do 
        user_id = users[:Users].first[:Id]
        get "/admin/projects/moshpit/users/#{user_id}"
        expect_json_types({ Name: :string, Email: :string })
      end

      it 'creates assets' do 
        post '/admin/projects/moshpit/assets', {
          :Assets => [
            { "Url" => "http://i.imgur.com/oX7fiqB.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/a/a1/Descendents_-_Milo_Goes_to_College_cover.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/6/67/Descendents_-_I_Don%27t_Want_to_Grow_Up_cover.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/e/ea/Descendents_-_All_cover.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/1/1e/Descendents_-_Everything_Sucks_cover.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/8/8d/Beelzebubba.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/d/dc/Eat_Your_Paisley%21.jpg" },
            { "Url" => "http://upload.wikimedia.org/wikipedia/en/7/7b/Big_Lizard_in_My_Backyard.jpg" }
          ]
        }
        expect_status 200
        expect_json_types({ Assets: :array_of_objects, Meta: :object })
      end

      it "makes an assignment" do
        user_id = users[:Users].first[:Id]
	      get "/projects/moshpit/tasks/oi/assignments", {'Cookie' => "moshpit_user_id=#{user_id}; moshpit_guest=true;"}
        expect_status 200
        asset_id = json_body[:Asset][:Id]
        assignment_id = json_body[:Id]
        expect_json_types({Id: :string, User: :string, Project: :string, Task: :string, Asset: :object })
        expect_json({:Asset=>{:Counts=>{:Assignments=>1, :Favorites=>0, :finished=>0, :skipped=>0, :unfinished=>1}}, :State=>"unfinished", :SubmittedData=>nil})
      end

      it "submits an assignment" do
        user_id = users[:Users].first[:Id]
	      post "/projects/moshpit/tasks/oi/assignments", { "Id" => "#{assignment_id}", "User" => "#{user_id}", "Project" => "moshpit", "Task" => "moshpit-oi", "Asset" => { "Id" => "#{asset_id}", "Project" => "moshpit", "Url" => "http://upload.wikipedia.org/en/e/ea/Descendents_-_All_cover.jpg", "Name" => "", "Metadata" => { }, "SubmittedData" => { "oi" => nil }, "Favorited" => false, "Verified" => false, "Counts" => { "Assignments" => 1, "Favorites" => 0, "finished" => 0, "skipped" => 0, "unfinished" => 1 } }, "State" => "finished", "SubmittedData" => { "punk-rocker" => "yes" } }, {'Cookie' => "moshpit_user_id=#{user_id}; moshpit_guest=true;"}
        expect_status 200
        expect_json_types({Id: :string, User: :string, Project: :string, Task: :string, Asset: :object })
        expect_json({:State=>"unfinished", :SubmittedData=>nil})
      end

      it "favorites and unfavorites an asset" do
        user_id = users[:Users].first[:Id]
	      get "/projects/moshpit/assets/#{asset_id}/favorite", {'Cookie' => "moshpit_user_id=#{user_id}; moshpit_guest=true;"}
        expect_status 200
        expect_json({:AssetId => asset_id, :Action => "favorited"})

	      get "/projects/moshpit/assets/#{asset_id}/favorite", {'Cookie' => "moshpit_user_id=#{user_id}; moshpit_guest=true;"}
        expect_status 200
        expect_json({:AssetId => asset_id, :Action => "unfavorited"})

	      get "/projects/moshpit/assets/#{asset_id}/favorite", {'Cookie' => "moshpit_user_id=#{user_id}; moshpit_guest=true;"}
        expect_status 200
        expect_json({:AssetId => asset_id, :Action => "favorited"})
      end

      it "returns assignments" do
        get '/admin/projects/moshpit/assignments?task=oi'
        expect_status 200
        expect_json_types({Assignments: :array_of_objects})
      end

      it "returns assignments for a state" do
        get '/admin/projects/moshpit/assignments?task=oi&state=unfinished'
        expect_status 200
        expect_json_types({Assignments: lambda { |assignments| expect(assignments.length).to eq(1)}})

        get '/admin/projects/moshpit/assignments?task=oi&state=finished'
        expect_status 200
        expect_json_types({Assignments: lambda { |assignments| expect(assignments.length).to eq(1)}})
      end

      it "paginates assignments" do
        get '/admin/projects/moshpit/assignments?task=oi&state=unfinished&from=0&size=1'
        expect_status 200
        expect_json_types({Assignments: lambda { |assignments| expect(assignments.length).to eq(1)}})

        get '/admin/projects/moshpit/assignments?task=oi&state=unfinished&from=0&size=0'
        expect_status 200
        expect_json_types({Assignments: lambda { |assignments| expect(assignments.length).to eq(0)}})
      end

      it 'finds all assets in a project' do 
        get '/admin/projects/moshpit/assets'
        expect_status 200
        expect_json_types({Assets: :array_of_objects, Meta: :object})
        expect_json_types('Assets.0', {Id: :string, Url: :string, Counts: :object})
      end

      it 'finds an asset in a project' do 
        get "/admin/projects/moshpit/assets/#{asset_id}"
        expect_status 200
        expect_json_types({Asset: {Id: :string, Url: :string, Counts: :object, SubmittedData: :object}})
      end

      it 'paginates assets' do 
        get '/admin/projects/moshpit/assets?from=0&size=1'
        expect_status 200
        expect_json_types({Assets: lambda { |assets| expect(assets.length).to eq(1)}})

        get '/admin/projects/moshpit/assets?from=1&size=3'
        expect_status 200
        expect_json_types({Assets: lambda { |assets| expect(assets.length).to eq(3)}})
      end


    end
  end
end
